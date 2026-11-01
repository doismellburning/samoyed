// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sqlite stores the BBS's messages in an SQLite database.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/routing"
	"github.com/doismellburning/samoyed/internal/maybe"
	_ "modernc.org/sqlite" // The "sqlite" database/sql driver
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	// ErrDuplicateBID says a message's ID has been seen before.
	ErrDuplicateBID = mail.ErrDuplicateBID

	// ErrNoSuchMessage says there is no message with that number.
	ErrNoSuchMessage = errors.New("no such message")
)

// Store is the BBS's message store.
type Store struct {
	db *sql.DB

	// now is the time messages are stamped with; a test can set it.
	now func() time.Time
}

// Open opens the database at file, creating it if need be, and brings its
// schema up to date.
func Open(ctx context.Context, file string) (*Store, error) {
	var dsn = new(url.URL)

	dsn.Scheme = "file"
	dsn.Opaque = file
	dsn.RawQuery = url.Values{"_pragma": {"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)"}}.Encode()

	var db, openErr = sql.Open("sqlite", dsn.String())
	if openErr != nil {
		return nil, fmt.Errorf("opening message store %s: %w", file, openErr)
	}

	// One connection: SQLite takes one writer at a time anyway, and this
	// way the BBS's writes queue here rather than fail as busy.
	db.SetMaxOpenConns(1)

	var migrateErr = migrate(ctx, db)
	if migrateErr != nil {
		_ = db.Close()

		return nil, fmt.Errorf("message store %s: %w", file, migrateErr)
	}

	var s = new(Store)

	s.db = db
	s.now = time.Now

	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate applies whichever of the embedded migrations the database hasn't
// had, each in a transaction of its own, counting them in user_version.
func migrate(ctx context.Context, db *sql.DB) error {
	var names, globErr = fs.Glob(migrations, "migrations/*.sql")
	if globErr != nil {
		return globErr
	}

	slices.Sort(names)

	var version int

	var versionErr = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	if versionErr != nil {
		return fmt.Errorf("reading schema version: %w", versionErr)
	}

	if version > len(names) {
		return fmt.Errorf("schema version %d is newer than this BBS knows (%d)", version, len(names))
	}

	for i, name := range names[version:] {
		var applyErr = applyMigration(ctx, db, name, version+i+1)
		if applyErr != nil {
			return fmt.Errorf("applying %s: %w", path.Base(name), applyErr)
		}
	}

	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, name string, version int) error {
	var script, readErr = migrations.ReadFile(name)
	if readErr != nil {
		return readErr
	}

	var tx, txErr = db.BeginTx(ctx, nil)
	if txErr != nil {
		return txErr
	}

	defer func() { _ = tx.Rollback() }()

	var _, execErr = tx.ExecContext(ctx, string(script))
	if execErr != nil {
		return execErr
	}

	// PRAGMA takes no parameters, so the version goes in as text; it is
	// only ever a count of our own files.
	var _, pragmaErr = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version))
	if pragmaErr != nil {
		return pragmaErr
	}

	return tx.Commit()
}

// Accept stores a new message to be kept here, numbering it and, if it has
// no ID yet, giving it one made from that number and bbsCall.  The message
// and its ID are recorded in one transaction: a message whose ID has been
// seen before is turned away with ErrDuplicateBID, and nothing changes.
func (s *Store) Accept(ctx context.Context, m mail.NewMessage, bbsCall string) (mail.Message, error) {
	return s.AcceptRouted(ctx, m, bbsCall, routing.Local())
}

// AcceptRouted is Accept for a message routing has decided about: it is
// queued for each partner the decision names, in the same transaction, and
// held for the sysop if the decision says so.
func (s *Store) AcceptRouted(ctx context.Context, m mail.NewMessage, bbsCall string, route routing.Decision) (mail.Message, error) {
	var validErr = m.Validate()
	if validErr != nil {
		return mail.Message{}, validErr
	}

	var tx, txErr = s.db.BeginTx(ctx, nil)
	if txErr != nil {
		return mail.Message{}, fmt.Errorf("storing message: %w", txErr)
	}

	defer func() { _ = tx.Rollback() }()

	var created = s.now().UTC().Truncate(time.Second)

	var status = mail.New
	if route.Held.IsJust() {
		status = mail.Held
	}

	var result, insertErr = tx.ExecContext(ctx,
		`INSERT INTO messages (type, status, from_call, to_call, at, title, body, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Type.String(), status.String(), strings.ToUpper(m.From), m.To.To, nullable(m.To.At), m.Title, m.Body, created.Unix())
	if insertErr != nil {
		return mail.Message{}, fmt.Errorf("storing message: %w", insertErr)
	}

	var id, idErr = result.LastInsertId()
	if idErr != nil {
		return mail.Message{}, fmt.Errorf("storing message: %w", idErr)
	}

	var number, numberErr = safecast.Convert[int](id)
	if numberErr != nil {
		return mail.Message{}, fmt.Errorf("storing message: %w", numberErr)
	}

	var mid = maybe.FromMaybe(mail.GenerateMID(number, bbsCall), m.MID)

	// Every message stored has its ID in the history, so nothing here
	// changing means the ID has been seen before.
	var bid, bidErr = tx.ExecContext(ctx, "INSERT INTO bid_history (bid, first_seen) VALUES (?, ?) ON CONFLICT DO NOTHING", mid, created.Unix())
	if bidErr != nil {
		return mail.Message{}, fmt.Errorf("recording message ID: %w", bidErr)
	}

	var added, addedErr = bid.RowsAffected()
	if addedErr != nil {
		return mail.Message{}, fmt.Errorf("recording message ID: %w", addedErr)
	}

	if added == 0 {
		return mail.Message{}, fmt.Errorf("%w: %s", ErrDuplicateBID, mid)
	}

	var _, midErr = tx.ExecContext(ctx, "UPDATE messages SET mid = ? WHERE number = ?", mid, number)
	if midErr != nil {
		return mail.Message{}, fmt.Errorf("recording message ID: %w", midErr)
	}

	if route.Held.IsNothing() {
		for _, partner := range route.Partners {
			var _, queueErr = tx.ExecContext(ctx, "INSERT INTO forward_queue (msg, partner, state) VALUES (?, ?, 'pending')", number, partner)
			if queueErr != nil {
				return mail.Message{}, fmt.Errorf("queueing message for %s: %w", partner, queueErr)
			}
		}
	}

	var commitErr = tx.Commit()
	if commitErr != nil {
		return mail.Message{}, fmt.Errorf("storing message: %w", commitErr)
	}

	var stored = new(mail.Message)

	stored.Number = number
	stored.MID = mid
	stored.Type = m.Type
	stored.Status = status
	stored.From = strings.ToUpper(m.From)
	stored.To = m.To
	stored.Title = m.Title
	stored.Size = len(m.Body)
	stored.Created = created
	stored.Body = m.Body

	return *stored, nil
}

// SeenBID says whether a message ID has been seen before, whether its message
// was taken or not.
func (s *Store) SeenBID(ctx context.Context, bid string) (bool, error) {
	var found int

	var err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bid_history WHERE bid = ?", bid).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("checking message ID: %w", err)
	}

	return found > 0, nil
}

const summaryColumns = "number, mid, type, status, from_call, to_call, at, title, length(body), created"

// Get returns message number n, or Nothing if there is none.  A killed
// message is still returned, with its status saying so.
func (s *Store) Get(ctx context.Context, n int) (maybe.Maybe[mail.Message], error) {
	var row = s.db.QueryRowContext(ctx, "SELECT "+summaryColumns+", body FROM messages WHERE number = ?", n)

	var m = new(mail.Message)

	var err = scanSummary(row, &m.Summary, &m.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return maybe.Nothing[mail.Message](), nil
	}

	if err != nil {
		return maybe.Nothing[mail.Message](), fmt.Errorf("reading message %d: %w", n, err)
	}

	return maybe.Just(*m), nil
}

// List returns the messages q asks for, newest first.
func (s *Store) List(ctx context.Context, q mail.ListQuery) ([]mail.Summary, error) {
	var where, args = listFilter(q)

	var rows, queryErr = s.db.QueryContext(ctx, "SELECT "+summaryColumns+" FROM messages WHERE "+where+" ORDER BY number DESC", args...)
	if queryErr != nil {
		return nil, fmt.Errorf("listing messages: %w", queryErr)
	}

	defer func() { _ = rows.Close() }()

	var summaries []mail.Summary

	for rows.Next() {
		var summary = new(mail.Summary)

		var scanErr = scanSummary(rows, summary)
		if scanErr != nil {
			return nil, fmt.Errorf("listing messages: %w", scanErr)
		}

		summaries = append(summaries, *summary)
	}

	var rowsErr = rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("listing messages: %w", rowsErr)
	}

	return summaries, nil
}

// listFilter turns q into a WHERE clause and its arguments.
func listFilter(q mail.ListQuery) (string, []any) {
	var clauses = []string{"1 = 1"}
	var args []any

	var in = func(column string, values []string) {
		clauses = append(clauses, column+" IN ("+strings.TrimSuffix(strings.Repeat("?, ", len(values)), ", ")+")")

		for _, v := range values {
			args = append(args, v)
		}
	}

	if len(q.Types) > 0 {
		in("type", stringsOf(q.Types))
	}

	if len(q.Statuses) > 0 {
		in("status", stringsOf(q.Statuses))
	} else {
		clauses = append(clauses, "status != ?")
		args = append(args, mail.Killed.String())
	}

	if from, ok := q.From.Get(); ok {
		clauses = append(clauses, "from_call = ?")
		args = append(args, strings.ToUpper(from))
	}

	if to, ok := q.To.Get(); ok {
		clauses = append(clauses, "to_call = ?")
		args = append(args, strings.ToUpper(to))
	}

	if lowest, ok := q.Lowest.Get(); ok {
		clauses = append(clauses, "number >= ?")
		args = append(args, lowest)
	}

	if highest, ok := q.Highest.Get(); ok {
		clauses = append(clauses, "number <= ?")
		args = append(args, highest)
	}

	return strings.Join(clauses, " AND "), args
}

func stringsOf[T fmt.Stringer](values []T) []string {
	var out = make([]string, 0, len(values))

	for _, v := range values {
		out = append(out, v.String())
	}

	return out
}

// SetStatus sets message n's status, for marking it read, killed, or held.
func (s *Store) SetStatus(ctx context.Context, n int, status mail.Status) error {
	var _, parseErr = mail.ParseStatus(status.String())
	if parseErr != nil {
		return parseErr
	}

	var result, err = s.db.ExecContext(ctx, "UPDATE messages SET status = ? WHERE number = ?", status.String(), n)
	if err != nil {
		return fmt.Errorf("updating message %d: %w", n, err)
	}

	var changed, _ = result.RowsAffected()
	if changed == 0 {
		return fmt.Errorf("%w: %d", ErrNoSuchMessage, n)
	}

	return nil
}

// scanner is a *sql.Row or *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanSummary reads summaryColumns, and then any more columns into extra.
func scanSummary(row scanner, summary *mail.Summary, extra ...any) error {
	var mid sql.NullString
	var typ, status string
	var at sql.NullString
	var created int64

	var dest = append([]any{&summary.Number, &mid, &typ, &status, &summary.From, &summary.To.To, &at, &summary.Title, &summary.Size, &created}, extra...)

	var scanErr = row.Scan(dest...)
	if scanErr != nil {
		return scanErr
	}

	var parsedType, typeErr = mail.ParseType(typ)
	if typeErr != nil {
		return typeErr
	}

	var parsedStatus, statusErr = mail.ParseStatus(status)
	if statusErr != nil {
		return statusErr
	}

	summary.MID = mid.String
	summary.Type = parsedType
	summary.Status = parsedStatus
	summary.To.At = maybe.Nothing[string]()

	if at.Valid {
		summary.To.At = maybe.Just(at.String)
	}

	summary.Created = time.Unix(created, 0).UTC()

	return nil
}

// nullable is m as a column value: NULL for Nothing.
func nullable(m maybe.Maybe[string]) sql.NullString {
	var value, ok = m.Get()

	return sql.NullString{String: value, Valid: ok}
}

// QueueFor is the messages waiting to be forwarded to partner, oldest first.
// Killed messages are left out: there is no point sending them.
func (s *Store) QueueFor(ctx context.Context, partner string) ([]int, error) {
	var rows, err = s.db.QueryContext(ctx,
		`SELECT q.msg FROM forward_queue q JOIN messages m ON m.number = q.msg
		 WHERE q.partner = ? AND q.state = 'pending' AND m.status != ?
		 ORDER BY q.msg`, strings.ToUpper(partner), mail.Killed.String())
	if err != nil {
		return nil, fmt.Errorf("reading the queue for %s: %w", partner, err)
	}

	defer func() { _ = rows.Close() }()

	var queue []int

	for rows.Next() {
		var n int

		var scanErr = rows.Scan(&n)
		if scanErr != nil {
			return nil, fmt.Errorf("reading the queue for %s: %w", partner, scanErr)
		}

		queue = append(queue, n)
	}

	return queue, rows.Err()
}

// RecordFailedAttempt records that partner couldn't be forwarded to, by
// counting an attempt against each message waiting for it.  A message that
// has been tried maxAttempts times stops waiting, and is held for the sysop:
// personal mail and traffic as a whole, since nothing else will deliver it;
// a bulletin only for that partner, as it is here to be read too.  It
// returns the numbers of the messages held.
func (s *Store) RecordFailedAttempt(ctx context.Context, partner string, maxAttempts int) ([]int, error) {
	var tx, txErr = s.db.BeginTx(ctx, nil)
	if txErr != nil {
		return nil, fmt.Errorf("recording a failed attempt: %w", txErr)
	}

	defer func() { _ = tx.Rollback() }()

	var call = strings.ToUpper(partner)

	var _, countErr = tx.ExecContext(ctx,
		"UPDATE forward_queue SET attempts = attempts + 1, last_try = ? WHERE partner = ? AND state = 'pending'",
		s.now().Unix(), call)
	if countErr != nil {
		return nil, fmt.Errorf("recording a failed attempt: %w", countErr)
	}

	var held, heldErr = triedOut(ctx, tx, call, maxAttempts)
	if heldErr != nil {
		return nil, fmt.Errorf("recording a failed attempt: %w", heldErr)
	}

	for _, n := range held {
		var _, holdErr = tx.ExecContext(ctx, "UPDATE forward_queue SET state = 'held' WHERE msg = ? AND partner = ?", n, call)
		if holdErr != nil {
			return nil, fmt.Errorf("holding message %d: %w", n, holdErr)
		}

		var _, statusErr = tx.ExecContext(ctx,
			"UPDATE messages SET status = ? WHERE number = ? AND type != 'B' AND status != ?",
			mail.Held.String(), n, mail.Killed.String())
		if statusErr != nil {
			return nil, fmt.Errorf("holding message %d: %w", n, statusErr)
		}
	}

	var commitErr = tx.Commit()
	if commitErr != nil {
		return nil, fmt.Errorf("recording a failed attempt: %w", commitErr)
	}

	return held, nil
}

// triedOut is the messages waiting for partner that have been tried
// maxAttempts times.
func triedOut(ctx context.Context, tx *sql.Tx, partner string, maxAttempts int) ([]int, error) {
	var rows, err = tx.QueryContext(ctx,
		"SELECT msg FROM forward_queue WHERE partner = ? AND state = 'pending' AND attempts >= ? ORDER BY msg", partner, maxAttempts)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var msgs []int

	for rows.Next() {
		var n int

		var scanErr = rows.Scan(&n)
		if scanErr != nil {
			return nil, scanErr
		}

		msgs = append(msgs, n)
	}

	return msgs, rows.Err()
}

// MarkForwarded records that partner has taken message n.
func (s *Store) MarkForwarded(ctx context.Context, n int, partner string) error {
	return s.finishForward(ctx, n, partner, "forwarded")
}

// MarkRefused records that partner turned message n down, as one it already
// has: it isn't offered to that partner again.
func (s *Store) MarkRefused(ctx context.Context, n int, partner string) error {
	return s.finishForward(ctx, n, partner, "refused")
}

// finishForward ends message n's place in partner's queue.  Personal mail and
// traffic are here only to be sent on, so once nobody is left to send one
// to, it is marked forwarded; a bulletin is kept here to be read as well, so
// it keeps its status.
func (s *Store) finishForward(ctx context.Context, n int, partner string, state string) error {
	var tx, txErr = s.db.BeginTx(ctx, nil)
	if txErr != nil {
		return fmt.Errorf("updating the queue: %w", txErr)
	}

	defer func() { _ = tx.Rollback() }()

	var result, updateErr = tx.ExecContext(ctx,
		"UPDATE forward_queue SET state = ?, attempts = attempts + 1, last_try = ? WHERE msg = ? AND partner = ? AND state = 'pending'",
		state, s.now().Unix(), n, strings.ToUpper(partner))
	if updateErr != nil {
		return fmt.Errorf("updating the queue: %w", updateErr)
	}

	var changed, _ = result.RowsAffected()
	if changed == 0 {
		return fmt.Errorf("%w: %d isn't waiting for %s", ErrNoSuchMessage, n, partner)
	}

	var _, statusErr = tx.ExecContext(ctx,
		`UPDATE messages SET status = ?
		 WHERE number = ? AND type != 'B' AND status NOT IN (?, ?)
		 AND NOT EXISTS (SELECT 1 FROM forward_queue WHERE msg = ? AND state = 'pending')`,
		mail.Forwarded.String(), n, mail.Killed.String(), mail.Held.String(), n)
	if statusErr != nil {
		return fmt.Errorf("updating message %d: %w", n, statusErr)
	}

	return tx.Commit()
}
