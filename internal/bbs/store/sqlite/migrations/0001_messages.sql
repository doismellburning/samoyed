-- SPDX-FileCopyrightText: 2026 The Samoyed Authors
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Messages, numbered in the order they arrived.  AUTOINCREMENT, so a number
-- is never reused once its message has gone.
CREATE TABLE messages (
	number INTEGER PRIMARY KEY AUTOINCREMENT,
	mid TEXT UNIQUE, -- Set in the same transaction as the insert
	type TEXT NOT NULL CHECK (type IN ('P', 'B', 'T')),
	status TEXT NOT NULL CHECK (status IN ('N', 'Y', 'F', 'K', 'H')),
	from_call TEXT NOT NULL,
	to_call TEXT NOT NULL,
	at TEXT, -- NULL for an address with no @
	title TEXT NOT NULL,
	body BLOB NOT NULL,
	created INTEGER NOT NULL -- Unix seconds
);

CREATE INDEX messages_to_call ON messages (to_call);
CREATE INDEX messages_from_call ON messages (from_call);

-- Every message ID taken or turned away, kept longer than the messages
-- themselves so a duplicate is still recognised after the first has expired.
CREATE TABLE bid_history (
	bid TEXT PRIMARY KEY,
	first_seen INTEGER NOT NULL -- Unix seconds
);
