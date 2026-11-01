-- SPDX-FileCopyrightText: 2026 The Samoyed Authors
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Each partner BBS a message is to be forwarded to, and how that is going.
CREATE TABLE forward_queue (
	msg INTEGER NOT NULL REFERENCES messages (number),
	partner TEXT NOT NULL,
	state TEXT NOT NULL CHECK (state IN ('pending', 'forwarded', 'refused')),
	attempts INTEGER NOT NULL DEFAULT 0,
	last_try INTEGER, -- Unix seconds; NULL until the first try
	PRIMARY KEY (msg, partner)
);

CREATE INDEX forward_queue_pending ON forward_queue (partner, state);
