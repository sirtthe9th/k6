package cmd

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/sirupsen/logrus"
)

// TODO move this to it's own package
// reconnect?
// filtering? limiting output? maybe? probably leave it for syslog-ng/rsyslog and co ?
// benchmark it
// buffer messages before sending them

// loosely based on https://godoc.org/github.com/sirupsen/logrus/hooks/syslog
type syslogHook struct {
	Writer           net.Conn
	SyslogNetwork    string
	SyslogRaddr      string
	additionalParams [][2]string
	ch               chan *logrus.Entry
	limit            int
	pushPeriod       time.Duration
}

func newSyslogHook(network, raddr string, limit int, additionalParams [][2]string) (*syslogHook, error) {
	w, err := net.Dial(network, raddr)
	if err != nil {
		return nil, err
	}

	h := &syslogHook{
		Writer:           w,
		SyslogNetwork:    network,
		SyslogRaddr:      raddr,
		additionalParams: additionalParams,
		ch:               make(chan *logrus.Entry, 1000),
		limit:            limit,           // TODO configurable,
		pushPeriod:       time.Second * 1, // TODO configurable,
	}

	go h.loop()

	return h, err
}

// fill one of two equally sized slices with entries and then push it while filling the other one
// TODO clean old entries after push?
// TODO this will be much faster if we can reuse rfc5424.Messages and they can use less intermediary
// buffers
func (hook *syslogHook) loop() {
	var (
		entrys             = make([]*logrus.Entry, hook.limit)
		entriesBeingPushed = make([]*logrus.Entry, hook.limit)
		dropped            int
		count              int
		ticker             = time.NewTicker(hook.pushPeriod)
		pushCh             = make(chan chan struct{})
	)

	defer close(pushCh)
	go func() {
		for ch := range pushCh {
			entriesBeingPushed, entrys = entrys, entriesBeingPushed
			oldCount, oldDropped := count, dropped
			count, dropped = 0, 0
			close(ch)
			_ = hook.push(entriesBeingPushed[:oldCount], oldDropped) // TODO print it on terminal ?!?
		}
	}()

	for {
		select {
		case entry, ok := <-hook.ch:
			if !ok {
				return
			}
			if count == hook.limit {
				dropped++
				continue
			}
			entrys[count] = entry
			count++
		case <-ticker.C:
			ch := make(chan struct{})
			pushCh <- ch
			<-ch
		}
	}
}

var b bytes.Buffer //nolint:nochecknoglobals // TODO maybe use sync.Pool?

func (hook *syslogHook) push(entrys []*logrus.Entry, dropped int) error {
	b.Reset()
	for _, entry := range entrys {
		if _, err := msgFromEntry(entry, hook.additionalParams).WriteTo(&b); err != nil {
			return err
		}
	}
	if dropped != 0 {
		_, err := msgFromEntry(
			&logrus.Entry{
				Data: logrus.Fields{
					"droppedCount": dropped,
				},
				Level: logrus.WarnLevel,
				Message: fmt.Sprintf("k6 dropped some packages because they were above the limit of %d/%s",
					hook.limit, hook.pushPeriod),
			},
			hook.additionalParams,
		).WriteTo(&b)
		if err != nil {
			return err
		}
	}
	_, err := b.WriteTo(hook.Writer)
	return err
}

func (hook *syslogHook) Fire(entry *logrus.Entry) error {
	hook.ch <- entry
	return nil
}

func msgFromEntry(entry *logrus.Entry, additionalParams [][2]string) rfc5424.Message {
	// TODO figure out if entrys share their entry.Data and use that to not recreate the same
	// sdParams
	sdParams := make([]rfc5424.SDParam, 1, 1+len(entry.Data)+len(additionalParams))
	sdParams[0] = rfc5424.SDParam{Name: "level", Value: entry.Level.String()}
	for name, value := range entry.Data {
		// TODO maybe do it only for some?
		// TODO have custom logic for different things ?
		sdParams = append(sdParams, rfc5424.SDParam{Name: name, Value: fmt.Sprint(value)})
	}

	for _, param := range additionalParams {
		sdParams = append(sdParams, rfc5424.SDParam{Name: param[0], Value: param[1]})
	}

	return rfc5424.Message{
		Priority:  rfc5424.Daemon | rfc5424.Info, // TODO figure this out
		Timestamp: entry.Time,
		Message:   []byte(entry.Message),
		StructuredData: []rfc5424.StructuredData{
			{
				ID:         "k6",
				Parameters: sdParams,
			},
		},
	}
}

func (hook *syslogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}
