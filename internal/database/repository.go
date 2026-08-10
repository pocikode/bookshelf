package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Repository struct{ DB *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{DB: db} }

func (r *Repository) GetPasswordCredential(ctx context.Context) (PasswordCredential, error) {
	var credential PasswordCredential
	var updated string
	err := r.DB.QueryRowContext(ctx, `SELECT password_digest,updated_at FROM password_credentials WHERE id=1`).Scan(&credential.Digest, &updated)
	if err != nil {
		return credential, err
	}
	credential.UpdatedAt, err = parseStamp(updated)
	return credential, err
}

func (r *Repository) FindUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	var created string
	err := r.DB.QueryRowContext(ctx, `SELECT id,username,role,created_at,disabled FROM users WHERE username=?`, username).Scan(&u.ID, &u.Username, &u.Role, &created, &u.Disabled)
	if err != nil {
		return u, err
	}
	u.CreatedAt, err = parseStamp(created)
	return u, err
}
func (r *Repository) FindUser(ctx context.Context, id int64) (User, error) {
	var u User
	var created string
	err := r.DB.QueryRowContext(ctx, `SELECT id,username,role,created_at,disabled FROM users WHERE id=?`, id).Scan(&u.ID, &u.Username, &u.Role, &created, &u.Disabled)
	if err != nil {
		return u, err
	}
	u.CreatedAt, err = parseStamp(created)
	return u, err
}
func (r *Repository) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id,username,role,created_at,disabled FROM users ORDER BY LOWER(username),id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var created string
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &created, &u.Disabled); err != nil {
			return nil, err
		}
		u.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (r *Repository) CreateUser(ctx context.Context, username, digest, role string, created time.Time) (User, error) {
	res, err := r.DB.ExecContext(ctx, `INSERT INTO users(username,password_digest,role,created_at) VALUES(?,?,?,?)`, username, digest, role, stamp(created))
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, Role: role, CreatedAt: created}, nil
}
func (r *Repository) SetUserPassword(ctx context.Context, id int64, digest string) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE users SET password_digest=? WHERE id=?`, digest, id)
	return err
}
func (r *Repository) GetUserPasswordDigest(ctx context.Context, id int64) (string, error) {
	var digest string
	err := r.DB.QueryRowContext(ctx, `SELECT password_digest FROM users WHERE id=?`, id).Scan(&digest)
	return digest, err
}
func (r *Repository) EnsureAdmin(ctx context.Context, digest string, created time.Time) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO users(id,username,password_digest,role,created_at) VALUES(1,'admin',?,'admin',?) ON CONFLICT(id) DO UPDATE SET password_digest=excluded.password_digest`, digest, stamp(created))
	return err
}
func (r *Repository) DeleteUserSessions(ctx context.Context, id int64) error {
	_, err := r.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id)
	return err
}
func (r *Repository) SetUserDisabled(ctx context.Context, id int64, disabled bool) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE users SET disabled=? WHERE id=?`, disabled, id)
	return err
}

func (r *Repository) SetPasswordCredential(ctx context.Context, credential PasswordCredential) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO password_credentials(id,password_digest,updated_at) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET password_digest=excluded.password_digest,updated_at=excluded.updated_at`, credential.Digest, stamp(credential.UpdatedAt))
	return err
}

func (r *Repository) CreateSession(ctx context.Context, s Session) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO sessions(token_hash,password_binding,created_at,last_seen_at,expires_at,user_agent,user_id) VALUES(?,?,?,?,?,?,?)`, s.TokenHash, s.PasswordBinding, stamp(s.CreatedAt), stamp(s.LastSeenAt), stamp(s.ExpiresAt), s.UserAgent, nullInt64(s.UserID))
	return err
}

func (r *Repository) FindSession(ctx context.Context, tokenHash string) (Session, error) {
	var s Session
	var created, seen, expires string
	err := r.DB.QueryRowContext(ctx, `SELECT token_hash,password_binding,created_at,last_seen_at,expires_at,COALESCE(user_agent,''),COALESCE(user_id,1) FROM sessions WHERE token_hash=?`, tokenHash).Scan(&s.TokenHash, &s.PasswordBinding, &created, &seen, &expires, &s.UserAgent, &s.UserID)
	if err != nil {
		return s, err
	}
	s.CreatedAt, err = parseStamp(created)
	if err != nil {
		return s, err
	}
	s.LastSeenAt, err = parseStamp(seen)
	if err != nil {
		return s, err
	}
	s.ExpiresAt, err = parseStamp(expires)
	return s, err
}

func (r *Repository) TouchSession(ctx context.Context, tokenHash string, at time.Time) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE token_hash=?`, stamp(at), tokenHash)
	return err
}
func (r *Repository) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}
func (r *Repository) DeleteAllSessions(ctx context.Context) error {
	_, err := r.DB.ExecContext(ctx, `DELETE FROM sessions`)
	return err
}
func (r *Repository) SweepSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=?`, stamp(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *Repository) InsertBook(ctx context.Context, b *Book) error {
	owner := nullInt64(b.OwnerID)
	res, err := r.DB.ExecContext(ctx, `INSERT INTO books(owner_id,is_public,title,author,category,format,file_hash,file_path,file_size,cover_path,page_count,language,publisher,created_at) VALUES(?,CASE WHEN ? IS NULL THEN 1 ELSE ? END,?,?,?,?,?,?,?,?,?,?,?,?)`, owner, owner, b.Public, b.Title, nullString(b.Author), b.Category, b.Format, b.FileHash, b.FilePath, b.FileSize, nullString(b.CoverPath), nullInt(b.PageCount), nullString(b.Language), nullString(b.Publisher), stamp(b.CreatedAt))
	if err != nil {
		return err
	}
	b.ID, err = res.LastInsertId()
	return err
}

const bookColumns = `b.id,COALESCE(b.owner_id,1),b.is_public,b.title,COALESCE(b.author,''),b.category,b.format,b.file_hash,b.file_path,b.file_size,COALESCE(b.cover_path,''),COALESCE(b.page_count,0),COALESCE(b.language,''),COALESCE(b.publisher,''),b.created_at,COALESCE(p.percent,0),p.updated_at`

func scanBook(scanner interface{ Scan(...any) error }) (Book, error) {
	var b Book
	var created string
	var last sql.NullString
	err := scanner.Scan(&b.ID, &b.OwnerID, &b.Public, &b.Title, &b.Author, &b.Category, &b.Format, &b.FileHash, &b.FilePath, &b.FileSize, &b.CoverPath, &b.PageCount, &b.Language, &b.Publisher, &created, &b.Percent, &last)
	if err != nil {
		return b, err
	}
	b.CreatedAt, err = parseStamp(created)
	if err != nil {
		return b, err
	}
	if last.Valid {
		t, e := parseStamp(last.String)
		if e != nil {
			return b, e
		}
		b.LastReadAt = &t
	}
	return b, nil
}

func (r *Repository) FindBook(ctx context.Context, id int64) (Book, error) {
	return scanBook(r.DB.QueryRowContext(ctx, `SELECT `+bookColumns+` FROM books b LEFT JOIN reading_progress p ON p.book_id=b.id WHERE b.id=?`, id))
}
func (r *Repository) FindBookForUser(ctx context.Context, id, userID int64, admin bool) (Book, error) {
	where := `b.id=? AND (b.is_public=1 OR b.owner_id=? OR ?=1)`
	return scanBook(r.DB.QueryRowContext(ctx, `SELECT `+bookColumns+` FROM books b LEFT JOIN reading_progress p ON p.book_id=b.id AND p.user_id=? WHERE `+where, userID, id, userID, admin))
}
func (r *Repository) FindBookByHash(ctx context.Context, hash string) (Book, error) {
	return scanBook(r.DB.QueryRowContext(ctx, `SELECT `+bookColumns+` FROM books b LEFT JOIN reading_progress p ON p.book_id=b.id WHERE b.file_hash=?`, hash))
}
func (r *Repository) FindBookByHashForUser(ctx context.Context, hash, userID int64, admin bool) (Book, error) {
	return scanBook(r.DB.QueryRowContext(ctx, `SELECT `+bookColumns+` FROM books b LEFT JOIN reading_progress p ON p.book_id=b.id AND p.user_id=? WHERE b.file_hash=? AND (b.is_public=1 OR b.owner_id=? OR ?=1)`, userID, hash, userID, admin))
}

func (r *Repository) ListBooks(ctx context.Context, o BookListOptions) ([]Book, int, error) {
	if o.Limit <= 0 || o.Limit > 60 {
		o.Limit = 60
	}
	if o.Page < 1 {
		o.Page = 1
	}
	sorts := map[string]string{"added": "b.created_at", "title": "LOWER(b.title)", "last_read": "COALESCE(p.updated_at,'')"}
	sortExpr, ok := sorts[o.Sort]
	if !ok {
		sortExpr = sorts["added"]
	}
	direction := "DESC"
	if strings.EqualFold(o.Direction, "asc") {
		direction = "ASC"
	}
	where := []string{"1=1"}
	args := []any{}
	if !o.Admin {
		where = append(where, "(b.is_public=1 OR b.owner_id=?)")
		args = append(args, o.UserID)
	}
	if o.Query != "" {
		q := "%" + escapeLike(o.Query) + "%"
		where = append(where, `(b.title LIKE ? ESCAPE '\' OR COALESCE(b.author,'') LIKE ? ESCAPE '\')`)
		args = append(args, q, q)
	}
	if o.Category != "" {
		where = append(where, "b.category=?")
		args = append(args, o.Category)
	}
	clause := strings.Join(where, " AND ")
	var count int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM books b WHERE `+clause, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	progressUser := o.UserID
	query := `SELECT ` + bookColumns + ` FROM books b LEFT JOIN reading_progress p ON p.book_id=b.id AND p.user_id=? WHERE ` + clause + ` ORDER BY ` + sortExpr + ` ` + direction + `, b.id DESC LIMIT ? OFFSET ?`
	queryArgs := append([]any{progressUser}, args...)
	rows, err := r.DB.QueryContext(ctx, query, append(queryArgs, o.Limit, (o.Page-1)*o.Limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	books := make([]Book, 0, o.Limit)
	for rows.Next() {
		b, e := scanBook(rows)
		if e != nil {
			return nil, 0, e
		}
		books = append(books, b)
	}
	return books, count, rows.Err()
}

func (r *Repository) ContinueReading(ctx context.Context) ([]Book, error) {
	return r.ContinueReadingForUser(ctx, 0, true)
}
func (r *Repository) ContinueReadingForUser(ctx context.Context, userID int64, admin bool) ([]Book, error) {
	visibility := "(b.is_public=1 OR b.owner_id=?)"
	args := []any{userID}
	if admin {
		visibility = "1=1"
		args = nil
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT `+bookColumns+` FROM books b JOIN reading_progress p ON p.book_id=b.id AND p.user_id=? WHERE p.percent>0 AND p.percent<0.995 AND `+visibility+` ORDER BY p.updated_at DESC LIMIT 3`, append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Book
	for rows.Next() {
		b, e := scanBook(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (r *Repository) Categories(ctx context.Context) ([]string, error) {
	return r.CategoriesForUser(ctx, 0, true)
}
func (r *Repository) CategoriesForUser(ctx context.Context, userID int64, admin bool) ([]string, error) {
	where, args := "1=1", []any{}
	if !admin {
		where, args = "(is_public=1 OR owner_id=?)", []any{userID}
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT DISTINCT category FROM books WHERE `+where+` ORDER BY LOWER(category),category`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *Repository) UpdateBook(ctx context.Context, id int64, title, author, category string) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE books SET title=?,author=?,category=? WHERE id=?`, title, nullString(author), category, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *Repository) UpdateBookVisibility(ctx context.Context, id int64, public bool) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE books SET is_public=? WHERE id=?`, public, id)
	return err
}
func (r *Repository) DeleteBookTx(ctx context.Context, id int64) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM books WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (r *Repository) GetProgress(ctx context.Context, bookID int64) (Progress, error) {
	return r.GetProgressForUser(ctx, 0, bookID)
}
func (r *Repository) GetProgressForUser(ctx context.Context, userID, bookID int64) (Progress, error) {
	if userID == 0 {
		userID = 1
	}
	var p Progress
	var updated string
	err := r.DB.QueryRowContext(ctx, `SELECT user_id,book_id,COALESCE(position,''),COALESCE(page,0),percent,COALESCE(device_label,''),updated_at FROM reading_progress WHERE user_id=? AND book_id=?`, userID, bookID).Scan(&p.UserID, &p.BookID, &p.Position, &p.Page, &p.Percent, &p.DeviceLabel, &updated)
	if err != nil {
		return p, err
	}
	p.UpdatedAt, err = parseStamp(updated)
	return p, err
}
func (r *Repository) UpsertProgress(ctx context.Context, p *Progress, preservePercent bool) error {
	return r.UpsertProgressForUser(ctx, p, preservePercent)
}
func (r *Repository) UpsertProgressForUser(ctx context.Context, p *Progress, preservePercent bool) error {
	if p.UserID == 0 {
		p.UserID = 1
	}
	var updated string
	var err error
	if preservePercent {
		err = r.DB.QueryRowContext(ctx, `INSERT INTO reading_progress(user_id,book_id,position,page,percent,device_label,updated_at) VALUES(?,?,?,?,?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(user_id,book_id) DO UPDATE SET position=excluded.position,page=excluded.page,device_label=excluded.device_label,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') RETURNING updated_at`, p.UserID, p.BookID, p.Position, nullInt(p.Page), 0, nullString(p.DeviceLabel)).Scan(&updated)
	} else {
		err = r.DB.QueryRowContext(ctx, `INSERT INTO reading_progress(user_id,book_id,position,page,percent,device_label,updated_at) VALUES(?,?,?,?,?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(user_id,book_id) DO UPDATE SET position=excluded.position,page=excluded.page,percent=excluded.percent,device_label=excluded.device_label,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') RETURNING updated_at`, p.UserID, p.BookID, p.Position, nullInt(p.Page), p.Percent, nullString(p.DeviceLabel)).Scan(&updated)
	}
	if err == nil {
		p.UpdatedAt, err = parseStamp(updated)
	}
	return err
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseStamp(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse database timestamp: %w", err)
	}
	return t.UTC(), nil
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
