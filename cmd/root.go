/*
 *
 * k6 - a next-generation load testing tool
 * Copyright (C) 2016 Load Impact
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package cmd

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/loadimpact/k6/lib/consts"
)

var BannerColor = color.New(color.FgCyan)

//TODO: remove these global variables
//nolint:gochecknoglobals
var (
	outMutex  = &sync.Mutex{}
	stdoutTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	stderrTTY = isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	stdout    = &consoleWriter{colorable.NewColorableStdout(), stdoutTTY, outMutex, nil}
	stderr    = &consoleWriter{colorable.NewColorableStderr(), stderrTTY, outMutex, nil}
)

const defaultConfigFileName = "config.json"

//TODO: remove these global variables
//nolint:gochecknoglobals
var defaultConfigFilePath = defaultConfigFileName // Updated with the user's config folder in the init() function below
//nolint:gochecknoglobals
var configFilePath = os.Getenv("K6_CONFIG") // Overridden by `-c`/`--config` flag!

//nolint:gochecknoglobals
var (
	//TODO: have environment variables for configuring these? hopefully after we move away from global vars though...
	verbose   bool
	quiet     bool
	noColor   bool
	logOutput string
	logFmt    string
	address   string
)

// RootCmd represents the base command when called without any subcommands.
var RootCmd = &cobra.Command{
	Use:           "k6",
	Short:         "a next-generation load generator",
	Long:          BannerColor.Sprintf("\n%s", consts.Banner),
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var logger = logrus.StandardLogger() // don't use the global one to begin with
		err := setupLoggers(logger, logFmt, logOutput)
		if err != nil {
			return err
		}

		if noColor {
			// TODO: figure out something else... currently, with the wrappers
			// below, we're stripping any colors from the output after we've
			// added them. The problem is that, besides being very inefficient,
			// this actually also strips other special characters from the
			// intended output, like the progressbar formatting ones, which
			// would otherwise be fine (in a TTY).
			//
			// It would be much better if we avoid messing with the output and
			// instead have a parametrized instance of the color library. It
			// will return colored output if colors are enabled and simply
			// return the passed input as-is (i.e. be a noop) if colors are
			// disabled...
			stdout.Writer = colorable.NewNonColorable(os.Stdout)
			stderr.Writer = colorable.NewNonColorable(os.Stderr)
		}
		log.SetOutput(logger.Writer())
		logger.Debugf("k6 version: v%s", consts.FullVersion())
		return nil
	},
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		code := -1
		var logger logrus.FieldLogger = logrus.StandardLogger()
		if e, ok := err.(ExitCode); ok {
			code = e.Code
			if e.Hint != "" {
				logger = logger.WithField("hint", e.Hint)
			}
		}
		logger.Error(err)
		os.Exit(code)
	}
}

func rootCmdPersistentFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("", pflag.ContinueOnError)
	//TODO: figure out a better way to handle the CLI flags - global variables are not very testable... :/
	flags.BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
	flags.BoolVarP(&quiet, "quiet", "q", false, "disable progress updates")
	flags.BoolVar(&noColor, "no-color", false, "disable colored output")
	flags.StringVar(&logOutput, "log-output", "stderr",
		"change output to which logs go, possible values are stderr,stdout,none,syslog[=host:port]")
	flags.StringVar(&logFmt, "logformat", "", "log output format")
	flags.StringVarP(&address, "address", "a", "localhost:6565", "address for the api server")

	//TODO: Fix... This default value needed, so both CLI flags and environment variables work
	flags.StringVarP(&configFilePath, "config", "c", configFilePath, "JSON config file")
	// And we also need to explicitly set the default value for the usage message here, so things
	// like `K6_CONFIG="blah" k6 run -h` don't produce a weird usage message
	flags.Lookup("config").DefValue = defaultConfigFilePath
	must(cobra.MarkFlagFilename(flags, "config"))
	return flags
}

func init() {
	confDir, err := os.UserConfigDir()
	if err != nil {
		logrus.WithError(err).Warn("could not get config directory")
		confDir = ".config"
	}
	defaultConfigFilePath = filepath.Join(
		confDir,
		"loadimpact",
		"k6",
		defaultConfigFileName,
	)

	RootCmd.PersistentFlags().AddFlagSet(rootCmdPersistentFlagSet())
}

// fprintf panics when where's an error writing to the supplied io.Writer
func fprintf(w io.Writer, format string, a ...interface{}) (n int) {
	n, err := fmt.Fprintf(w, format, a...)
	if err != nil {
		panic(err.Error())
	}
	return n
}

// RawFormater it does nothing with the message just prints it
type RawFormater struct{}

// Format renders a single log entry
func (f RawFormater) Format(entry *logrus.Entry) ([]byte, error) {
	return append([]byte(entry.Message), '\n'), nil
}

func setupLoggers(logger *logrus.Logger, logFmt string, logOutput string) error {
	if verbose {
		logger.SetLevel(logrus.DebugLevel)
	}
	switch logOutput {
	case "stderr":
		logger.SetOutput(stderr)
	case "stdout":
		logger.SetOutput(stdout)
	case "none":
		logger.SetOutput(ioutil.Discard)
	default:
		if !strings.HasPrefix(logOutput, "syslog") {
			return fmt.Errorf("unsupported log output `%s`", logOutput)
		}
		var protocol = "tcp"
		var addr = "localhost:514"
		var additionalParams [][2]string
		var limit = 100
		if logOutput != "syslog" {
			parts := strings.SplitN(logOutput, "=", 2)
			if parts[0] != "syslog" {
				return fmt.Errorf("syslog  configuration should be in the form `syslog=host:port` but is `%s`", logOutput)
			}
			args := strings.Split(parts[1], ",")
			addr = args[0]
			// TODO use something better ... maybe
			// https://godoc.org/github.com/kubernetes/helm/pkg/strvals
			// atleast until https://github.com/loadimpact/k6/issues/926?
			if len(args) > 1 {
				for _, arg := range args[1:] {
					paramParts := strings.SplitN(arg, "=", 2)
					key := paramParts[0]
					value := paramParts[1]
					switch key {
					case "additionalParams":
						values := strings.Split(value, ";") // ; because , is already used

						additionalParams = make([][2]string, len(values)-1)
						for i, value := range values[1:] {
							paramParts := strings.SplitN(value, "=", 2)
							additionalParams[i] = [2]string{paramParts[0], paramParts[1]}
						}
					case "limit":
						var err error
						limit, err = strconv.Atoi(value)
						if err != nil {
							return fmt.Errorf("couldn't parse the syslog limit as a number %w", err)
						}
					default:
						return fmt.Errorf("unknown syslog config key %s", key)
					}
				}
			}
		}

		hook, err := newSyslogHook(protocol, addr, limit, additionalParams)
		if err != nil {
			return err
		}
		logger.AddHook(hook)
		logger.SetOutput(ioutil.Discard) // don't output to anywhere else
		logFmt = "raw"                   // TODO try do syslog formatting through a formatter and just write it to the net.Conn
		noColor = true                   // disable color
	}

	switch logFmt {
	case "raw":
		logger.SetFormatter(&RawFormater{})
		logger.Debug("Logger format: RAW")
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{})
		logger.Debug("Logger format: JSON")
	default:
		logger.SetFormatter(&logrus.TextFormatter{ForceColors: stderrTTY, DisableColors: noColor})
		logger.Debug("Logger format: TEXT")
	}
	return nil
}
