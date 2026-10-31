// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// An external KISS TNC - an SCHANNEL or NCHANNEL one - runs its own channel
// access, but many leave the settings for it to the host: a NinoTNC expects
// KISS commands to set its slot time and persistence, and pdn-soundmodem has
// no setting for any of them but KISS.  TNCChannelAccess is what we tell one.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

// TNCChannelAccess is the channel access settings for an external KISS TNC,
// each in the units its KISS command carries: 10 ms for the times, and a
// chance out of 256 for the persistence.  A setting that is Nothing has not
// been given, so the TNC is left to its own value for it.
type TNCChannelAccess struct {
	TXDelay  maybe.Maybe[byte]
	Persist  maybe.Maybe[byte]
	SlotTime maybe.Maybe[byte]
	TXTail   maybe.Maybe[byte]
}

// tncChannelAccessSetter changes some of the channel access settings of the
// external TNC on channel - those that are Just in access - and sends them to
// it.  A channel with no external TNC ignores it.
type tncChannelAccessSetter func(channel int, access TNCChannelAccess)

// updatedBy is a with every setting that is Just in update replaced by it.
func (a TNCChannelAccess) updatedBy(update TNCChannelAccess) TNCChannelAccess {
	return TNCChannelAccess{
		TXDelay:  update.TXDelay.Or(a.TXDelay),
		Persist:  update.Persist.Or(a.Persist),
		SlotTime: update.SlotTime.Or(a.SlotTime),
		TXTail:   update.TXTail.Or(a.TXTail),
	}
}

// kissFrames is the KISS command frames, framing and escapes included, that
// give a TNC each of the settings that is Just, in the order TXDELAY,
// persistence, slot time, TXTAIL.  Like the data frames we send it, each is
// for the TNC's only radio, KISS port 0, whatever channel it is to us.
func (a TNCChannelAccess) kissFrames() []byte {
	var frames []byte

	for _, setting := range []struct {
		command byte
		value   maybe.Maybe[byte]
	}{
		{kiss.CmdTxDelay, a.TXDelay},
		{kiss.CmdPersistence, a.Persist},
		{kiss.CmdSlotTime, a.SlotTime},
		{kiss.CmdTxTail, a.TXTail},
	} {
		if value, ok := setting.value.Get(); ok {
			frames = append(frames, kiss.Encapsulate([]byte{setting.command & 0x0f, value})...)
		}
	}

	return frames
}

// logFields describes the settings that are Just, for a log entry.
func (a TNCChannelAccess) logFields() logrus.Fields {
	var fields = make(logrus.Fields)

	for _, setting := range []struct {
		name  string
		value maybe.Maybe[byte]
	}{
		{"txdelay", a.TXDelay},
		{"persist", a.Persist},
		{"slottime", a.SlotTime},
		{"txtail", a.TXTail},
	} {
		if value, ok := setting.value.Get(); ok {
			fields[setting.name] = value
		}
	}

	return fields
}

// TNCChannelAccessSettings is the channel access settings an NCHANNEL or
// SCHANNEL gives its TNC, as a configuration file writes them: each in the
// units of its KISS command, 0 to 255, and nil when not given.
type TNCChannelAccessSettings struct {
	TXDelay  *int `yaml:"txdelay"`
	Persist  *int `yaml:"persist"`
	SlotTime *int `yaml:"slottime"`
	TXTail   *int `yaml:"txtail"`
}

// parseOption takes option, a KEYWORD=value word from directive's line, into
// s.  The keywords are TXDELAY, PERSIST, SLOTTIME and TXTAIL, in any case.
func (s *TNCChannelAccessSettings) parseOption(line int, directive string, option string) error {
	var keyword, text, found = strings.Cut(option, "=")
	if !found {
		return fmt.Errorf("line %d: Expected KEYWORD=value, not \"%s\", for %s command", line, option, directive)
	}

	var value, err = strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("line %d: Value for %s must be numeric, not \"%s\", for %s command", line, keyword, text, directive)
	}

	switch strings.ToUpper(keyword) {
	case "TXDELAY":
		s.TXDelay = &value
	case "PERSIST":
		s.Persist = &value
	case "SLOTTIME":
		s.SlotTime = &value
	case "TXTAIL":
		s.TXTail = &value
	default:
		return fmt.Errorf("line %d: Unknown option \"%s\" for %s command.  Expected TXDELAY, PERSIST, SLOTTIME or TXTAIL", line, keyword, directive)
	}

	return nil
}

// channelAccess checks s, for directive's line, and turns it into the
// settings to send the TNC.
func (s *TNCChannelAccessSettings) channelAccess(line int, directive string) (TNCChannelAccess, error) {
	var access TNCChannelAccess

	for _, setting := range []struct {
		name  string
		value *int
		into  *maybe.Maybe[byte]
	}{
		{"TXDELAY", s.TXDelay, &access.TXDelay},
		{"PERSIST", s.Persist, &access.Persist},
		{"SLOTTIME", s.SlotTime, &access.SlotTime},
		{"TXTAIL", s.TXTail, &access.TXTail},
	} {
		if setting.value == nil {
			continue
		}

		var value = *setting.value
		if value < 0 || value > 255 {
			return access, fmt.Errorf("line %d: %s for %s command must be in range 0 to 255, not %d", line, setting.name, directive, value)
		}

		*setting.into = maybe.Just(byte(value & 0xff))
	}

	return access, nil
}
