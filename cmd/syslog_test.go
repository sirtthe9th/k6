/*
 *
 * k6 - a next-generation load testing tool
 * Copyright (C) 2020 Load Impact
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
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestSyslogFromConfigLine(t *testing.T) {
	t.Parallel()
	tests := [...]struct {
		line string
		err  bool
		res  syslogHook
	}{
		{
			line: "syslog", // default settings
			res: syslogHook{
				protocol:   "tcp",
				addr:       "localhost:514",
				limit:      100,
				pushPeriod: time.Second * 1,
				levels:     logrus.AllLevels,
			},
		},
		{
			line: "syslog=somewhere:1233,additionalParams=something=else;foo=bar,limit=32,level=debug",
			res: syslogHook{
				protocol:         "tcp",
				addr:             "somewhere:1233",
				limit:            32,
				pushPeriod:       time.Second * 1,
				levels:           []logrus.Level{logrus.DebugLevel, logrus.InfoLevel, logrus.WarnLevel, logrus.ErrorLevel},
				additionalParams: [][2]string{{"something", "else"}, {"foo", "bar"}},
			},
		},
		{
			line: "syslogno",
			err:  true,
		},
		{
			line: "syslog=something,limit=word",
			err:  true,
		},
		{
			line: "syslog=something,level=notlevel",
			err:  true,
		},
		{
			line: "syslog=something,unknownoption",
			err:  true,
		},
		{
			line: "syslog=something,additionalParams=somethng",
			err:  true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.line, func(t *testing.T) {
			// no parallel because this is way too fast and parallel will only slow it down

			res, err := syslogFromConfigLine(test.line)

			if test.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, &test.res, res)
		})
	}
}
