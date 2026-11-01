-- SPDX-FileCopyrightText: 2026 The Samoyed Authors
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- A message a partner couldn't be reached for, time after time, is held
-- there for the sysop.  SQLite can't change a CHECK, so the table is made
-- afresh with the new state allowed.
CREATE TABLE forward_queue_new (
	msg INTEGER NOT NULL REFERENCES messages (number),
	partner TEXT NOT NULL,
	state TEXT NOT NULL CHECK (state IN ('pending', 'forwarded', 'refused', 'held')),
	attempts INTEGER NOT NULL DEFAULT 0,
	last_try INTEGER, -- Unix seconds; NULL until the first try
	PRIMARY KEY (msg, partner)
);

INSERT INTO forward_queue_new (msg, partner, state, attempts, last_try)
	SELECT msg, partner, state, attempts, last_try FROM forward_queue;

DROP TABLE forward_queue;

ALTER TABLE forward_queue_new RENAME TO forward_queue;

CREATE INDEX forward_queue_pending ON forward_queue (partner, state);
