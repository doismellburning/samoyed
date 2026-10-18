// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"github.com/doismellburning/samoyed/internal/chat"
	"github.com/doismellburning/samoyed/internal/maybe"
)

// chatConfigFor checks the node's chat settings, nil for no chat server.
func chatConfigFor(settings *ChatSettings) (maybe.Maybe[chat.Config], error) {
	if settings == nil {
		return maybe.Nothing[chat.Config](), nil
	}

	var cfg = chat.DefaultConfig()
	if settings.DefaultRoom != "" {
		cfg.DefaultRoom = settings.DefaultRoom
	}

	setIf(&cfg.RateLines, settings.RateLines)
	setIf(&cfg.RateWindow, settings.RateWindow)

	var _, err = chat.New(cfg)
	if err != nil {
		return maybe.Nothing[chat.Config](), err
	}

	return maybe.Just(cfg), nil
}

// nodeChatConfig is the node's chat server's configuration, or Nothing for
// none.
func nodeChatConfig(misc *misc_config_s) maybe.Maybe[chat.Config] {
	return maybe.Bind(misc.node, func(settings nodeSettings) maybe.Maybe[chat.Config] { return settings.chat })
}
