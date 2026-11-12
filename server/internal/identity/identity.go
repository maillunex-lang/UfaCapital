package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"

	"github.com/maillunex-lang/UfaCapital/server/internal/domain"
)

type Service struct {
	Pool         *pgxpool.Pool
	ArgonMemory  uint32
	ArgonTime    uint32
	ArgonThreads uint8
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
}

type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	IsAI        bool
	Roles       []string
	VIPUntil    *time.Time
}

type Session struct {
	Access    string
	Refresh   string
	ExpiresAt time.Time
	User      User
	CSRF      string
	SessionID uuid.UUID
}

func (s *Service) params() (uint32, uint32, uint8) {
	m, t, p := s.ArgonMemory, s.ArgonTime, s.ArgonThreads
	if m == 0 {
		m = 19 * 1024
	}
	if t == 0 {
		t = 2
	}
	if p == 0 {
		p = 1
	}
	return m, t, p
}

func HashPassword(pw string, memory, timeCost uint32, threads uint8) (string, error) {
	if len(pw) < 12 || len(pw) > 128 {
		return "", domain.E(400, "BAD_PASSWORD", "пароль должен быть от 12 до 128 символов")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(pw), salt, timeCost, memory, threads, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, timeCost, threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum)), nil
}

func verifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	sum := argon2.IDKey([]byte(pw), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(sum, want) == 1
}

func (s *Service) Register(ctx context.Context, email, password, display string) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	display = strings.TrimSpace(display)
	if !strings.Contains(email, "@") || strings.HasSuffix(email, "@agents.ufacapital.invalid") || strings.HasSuffix(email, "@system.ufacapital.invalid") {
		return Session{}, domain.E(400, "BAD_EMAIL", "некорректная почта")
	}
	if display == "" || len(display) > 80 {
		return Session{}, domain.E(400, "BAD_NAME", "укажите имя")
	}
	if err := s.rateLimit(ctx, email, false); err != nil {
		return Session{}, err
	}
	m, t, p := s.params()
	hash, err := HashPassword(password, m, t, p)
	if err != nil {
		return Session{}, err
	}
	id := uuid.New()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO users(id, email, password_hash, display_name) VALUES ($1,$2,$3,$4)`, id, email, hash, display)
	if err != nil {
		if isUnique(err) {
			_ = s.noteAttempt(ctx, email, false)
			return Session{}, domain.E(409, "EMAIL_TAKEN", "почта уже зарегистрирована")
		}
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role) VALUES ($1,'player')`, id); err != nil {
		return Session{}, err
	}
	sess, err := s.issue(ctx, tx, id, "api")
	if err != nil {
		return Session{}, err
	}
	if err := s.noteAttemptTx(ctx, tx, email, true); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return sess, nil
}

func (s *Service) Login(ctx context.Context, email, password, channel string) (Session, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.rateLimit(ctx, email, true); err != nil {
		return Session{}, false, err
	}
	var id uuid.UUID
	var hash string
	var blocked *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT id, password_hash, blocked_at FROM users WHERE email=$1`, email).Scan(&id, &hash, &blocked)
	if err != nil || !verifyPassword(password, hash) {
		_ = s.noteAttempt(ctx, email, false)
		return Session{}, false, domain.E(401, "BAD_LOGIN", "неверная почта или пароль")
	}
	if blocked != nil {
		return Session{}, false, domain.E(403, "BLOCKED", "учётная запись заблокирована")
	}
	if channel == "admin" {
		ok, err := s.userHasAny(ctx, id, "admin_read", "catalog_editor", "economy_admin", "moderator", "asset_editor", "superadmin")
		if err != nil {
			return Session{}, false, err
		}
		if !ok {
			_ = s.noteAttempt(ctx, email, false)
			return Session{}, false, domain.E(403, "FORBIDDEN", "нет прав администратора")
		}
		var enabled bool
		err = s.Pool.QueryRow(ctx, `SELECT enabled FROM user_totp WHERE user_id=$1`, id).Scan(&enabled)
		if err != nil || !enabled {
			return Session{}, false, domain.E(403, "MFA_ENROLL", "для админки нужен включённый второй фактор")
		}
		token, err := s.createChallenge(ctx, id)
		if err != nil {
			return Session{}, false, err
		}
		_ = s.noteAttempt(ctx, email, true)
		return Session{Access: token, User: User{ID: id, Email: email}}, true, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Session{}, false, err
	}
	defer tx.Rollback(ctx)
	sess, err := s.issue(ctx, tx, id, "api")
	if err != nil {
		return Session{}, false, err
	}
	if err := s.noteAttemptTx(ctx, tx, email, true); err != nil {
		return Session{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, false, err
	}
	return sess, false, nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, challenge, code string) (Session, error) {
	sum := sha256.Sum256([]byte(challenge))
	var userID uuid.UUID
	var expires time.Time
	err := s.Pool.QueryRow(ctx, `SELECT user_id, expires_at FROM mfa_challenges WHERE token_hash=$1`, sum[:]).Scan(&userID, &expires)
	if err != nil || time.Now().After(expires) {
		return Session{}, domain.E(401, "BAD_MFA", "код не принят")
	}
	var secret []byte
	if err := s.Pool.QueryRow(ctx, `SELECT secret FROM user_totp WHERE user_id=$1 AND enabled`, userID).Scan(&secret); err != nil {
		return Session{}, domain.E(401, "BAD_MFA", "код не принят")
	}
	if !totpOK(secret, code, time.Now()) {
		return Session{}, domain.E(401, "BAD_MFA", "код не принят")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM mfa_challenges WHERE token_hash=$1`, sum[:]); err != nil {
		return Session{}, err
	}
	sess, err := s.issue(ctx, tx, userID, "admin")
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return sess, nil
}

func (s *Service) EnrollTOTP(ctx context.Context, userID uuid.UUID) (string, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO user_totp(user_id, secret, enabled) VALUES ($1,$2,true)
		ON CONFLICT (user_id) DO UPDATE SET secret=EXCLUDED.secret, enabled=true`, userID, secret)
	if err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

func (s *Service) Refresh(ctx context.Context, refresh string) (Session, error) {
	sum := sha256.Sum256([]byte(refresh))
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	var sessionID, family, userID uuid.UUID
	var expires time.Time
	var used, revoked *time.Time
	err = tx.QueryRow(ctx, `SELECT session_id, family_id, expires_at, used_at, revoked_at FROM refresh_tokens WHERE hash=$1 FOR UPDATE`, sum[:]).
		Scan(&sessionID, &family, &expires, &used, &revoked)
	if err != nil {
		return Session{}, domain.E(401, "BAD_REFRESH", "сессия недействительна")
	}
	if used != nil {
		_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE family_id=$1 AND revoked_at IS NULL`, family)
		_, _ = tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE family_id=$1 AND revoked_at IS NULL`, family)
		_ = tx.Commit(ctx)
		return Session{}, domain.E(401, "BAD_REFRESH", "сессия недействительна")
	}
	if revoked != nil || time.Now().After(expires) {
		return Session{}, domain.E(401, "BAD_REFRESH", "сессия недействительна")
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET used_at=now() WHERE hash=$1`, sum[:]); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, sessionID); err != nil {
		return Session{}, err
	}
	var channel string
	if err := tx.QueryRow(ctx, `SELECT user_id, channel FROM sessions WHERE id=$1`, sessionID).Scan(&userID, &channel); err != nil {
		return Session{}, err
	}
	sess, err := s.issueFamily(ctx, tx, userID, channel, family)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return sess, nil
}

func (s *Service) Logout(ctx context.Context, access string) error {
	sum := sha256.Sum256([]byte(access))
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE access_hash=$1 AND revoked_at IS NULL`, sum[:])
	return err
}

func (s *Service) Authenticate(ctx context.Context, access, channel string) (User, string, uuid.UUID, error) {
	sum := sha256.Sum256([]byte(access))
	var userID, sessionID uuid.UUID
	var expires time.Time
	var gotChannel string
	var csrf *string
	err := s.Pool.QueryRow(ctx, `SELECT id, user_id, channel, access_expires_at, csrf_secret FROM sessions
		WHERE access_hash=$1 AND revoked_at IS NULL`, sum[:]).Scan(&sessionID, &userID, &gotChannel, &expires, &csrf)
	if err != nil || time.Now().After(expires) || (channel != "" && gotChannel != channel) {
		return User{}, "", uuid.Nil, domain.E(401, "NO_SESSION", "нужна сессия")
	}
	u, err := s.loadUser(ctx, s.Pool, userID)
	if err != nil {
		return User{}, "", uuid.Nil, err
	}
	secret := ""
	if csrf != nil {
		secret = *csrf
	}
	return u, secret, sessionID, nil
}

func (s *Service) loadUser(ctx context.Context, q rowQuery, id uuid.UUID) (User, error) {
	var u User
	err := q.QueryRow(ctx, `SELECT id, email, display_name, is_ai FROM users WHERE id=$1 AND blocked_at IS NULL`, id).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsAI)
	if err != nil {
		return User{}, domain.E(401, "NO_SESSION", "нужна сессия")
	}
	rows, err := q.Query(ctx, `SELECT role FROM user_roles WHERE user_id=$1 ORDER BY role`, id)
	if err != nil {
		return User{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return User{}, err
		}
		u.Roles = append(u.Roles, role)
	}
	var until time.Time
	err = q.QueryRow(ctx, `SELECT expires_at FROM vip_entitlements
		WHERE user_id=$1 AND revoked_at IS NULL AND starts_at<=now() AND expires_at>now()
		ORDER BY expires_at DESC LIMIT 1`, id).Scan(&until)
	if err == nil {
		u.VIPUntil = &until
	}
	return u, nil
}

func (s *Service) Has(u User, roles ...string) bool {
	if contains(u.Roles, "superadmin") {
		return true
	}
	for _, need := range roles {
		if contains(u.Roles, need) {
			return true
		}
	}
	return false
}

func (s *Service) issue(ctx context.Context, tx pgx.Tx, userID uuid.UUID, channel string) (Session, error) {
	return s.issueFamily(ctx, tx, userID, channel, uuid.New())
}

func (s *Service) issueFamily(ctx context.Context, tx pgx.Tx, userID uuid.UUID, channel string, family uuid.UUID) (Session, error) {
	access, accessHash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	refresh, refreshHash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	csrf, _, err := newToken()
	if err != nil {
		return Session{}, err
	}
	accessTTL := s.AccessTTL
	if accessTTL == 0 {
		accessTTL = 15 * time.Minute
	}
	refreshTTL := s.RefreshTTL
	if refreshTTL == 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	sid := uuid.New()
	exp := time.Now().Add(accessTTL)
	_, err = tx.Exec(ctx, `INSERT INTO sessions(id, user_id, family_id, channel, access_hash, access_expires_at, csrf_secret)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, sid, userID, family, channel, accessHash, exp, csrf)
	if err != nil {
		return Session{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO refresh_tokens(hash, session_id, family_id, expires_at) VALUES ($1,$2,$3,$4)`,
		refreshHash, sid, family, time.Now().Add(refreshTTL))
	if err != nil {
		return Session{}, err
	}
	u, err := s.loadUser(ctx, tx, userID)
	if err != nil {
		return Session{}, err
	}
	return Session{Access: access, Refresh: refresh, ExpiresAt: exp, User: u, CSRF: csrf, SessionID: sid}, nil
}

func (s *Service) createChallenge(ctx context.Context, userID uuid.UUID) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO mfa_challenges(id, user_id, token_hash, expires_at) VALUES ($1,$2,$3,$4)`,
		uuid.New(), userID, hash, time.Now().Add(5*time.Minute))
	return raw, err
}

func (s *Service) rateLimit(ctx context.Context, email string, lock bool) error {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM login_attempts
		WHERE email=$1 AND success=false AND created_at > now() - interval '10 minutes'`, email).Scan(&n)
	if err != nil {
		return err
	}
	if n >= 8 {
		return domain.E(429, "RATE_LIMIT", "слишком много попыток, подождите")
	}
	_ = lock
	return nil
}

func (s *Service) noteAttempt(ctx context.Context, email string, ok bool) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO login_attempts(email, success) VALUES ($1,$2)`, email, ok)
	return err
}

func (s *Service) noteAttemptTx(ctx context.Context, tx pgx.Tx, email string, ok bool) error {
	_, err := tx.Exec(ctx, `INSERT INTO login_attempts(email, success) VALUES ($1,$2)`, email, ok)
	return err
}

func (s *Service) userHasAny(ctx context.Context, id uuid.UUID, roles ...string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles WHERE user_id=$1 AND (role='superadmin' OR role = ANY($2)))`, id, roles).Scan(&ok)
	return ok, err
}

func (s *Service) SetRoles(ctx context.Context, tx pgx.Tx, actor User, target uuid.UUID, roles []string, reason string) error {
	if !s.Has(actor, "superadmin") {
		return domain.E(403, "FORBIDDEN", "роли меняет только суперадмин")
	}
	if actor.ID == target {
		return domain.E(403, "FORBIDDEN", "нельзя менять собственные роли")
	}
	if strings.TrimSpace(reason) == "" {
		return domain.E(400, "REASON_REQUIRED", "нужна причина")
	}
	allowed := map[string]bool{
		"player": true, "admin_read": true, "catalog_editor": true, "economy_admin": true,
		"moderator": true, "asset_editor": true, "test_operator": true, "superadmin": true,
	}
	for _, r := range roles {
		if !allowed[r] {
			return domain.E(400, "BAD_ROLE", "неизвестная роль")
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, target); err != nil {
		return err
	}
	for _, r := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role) VALUES ($1,$2)`, target, r); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RevokeSession(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, sessionID)
	return err
}

type rowQuery interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	text := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(text))
	return text, sum[:], nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

func totpOK(secret []byte, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for _, delta := range []int64{-1, 0, 1} {
		if subtle.ConstantTimeCompare([]byte(totpAt(secret, now.Add(time.Duration(delta)*30*time.Second))), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

func totpAt(secret []byte, t time.Time) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(t.Unix()/30))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", bin%1000000)
}

// DebugTOTP is for tests that enrolled a known secret.
func DebugTOTP(secretB32 string, t time.Time) (string, error) {
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secretB32))
	if err != nil {
		return "", err
	}
	return totpAt(secret, t), nil
}

func RandomPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "Aa1!" + hex.EncodeToString(b), nil
}

var ErrNoRows = errors.New("no rows")

// BootstrapAdmin creates the first superadmin with TOTP already enrolled and
// returns the base32 secret once. It refuses to run if any superadmin exists,
// so it cannot be used to add a second way in.
func (s *Service) BootstrapAdmin(ctx context.Context, email, password, display string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return "", domain.E(400, "BAD_EMAIL", "некорректная почта")
	}
	if len(password) < 16 {
		return "", domain.E(400, "WEAK_PASSWORD", "пароль администратора короче 16 символов")
	}
	m, t, p := s.params()
	hash, err := HashPassword(password, m, t, p)
	if err != nil {
		return "", err
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// One bootstrap at a time: the check below and the insert must not interleave.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7309001)`); err != nil {
		return "", err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles WHERE role='superadmin')`).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return "", domain.E(409, "ADMIN_EXISTS", "суперадмин уже есть")
	}
	id := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO users(id, email, password_hash, display_name) VALUES ($1,$2,$3,$4)`, id, email, hash, display); err != nil {
		if isUnique(err) {
			return "", domain.E(409, "EMAIL_TAKEN", "почта уже зарегистрирована")
		}
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role) VALUES ($1,'player'), ($1,'superadmin')`, id); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_totp(user_id, secret, enabled) VALUES ($1,$2,true)`, id, secret); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(id, actor_id, action, reason, after_state, correlation_id)
		VALUES ($1, NULL, 'bootstrap_admin', 'первый суперадмин создан командой на сервере', $2, $3)`,
		uuid.New(), `{"user":"`+id.String()+`"}`, uuid.New()); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}
