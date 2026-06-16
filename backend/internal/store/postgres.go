package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) ListConversations(ctx context.Context) ([]Conversation, error) {
	userID := UserIDFromContext(ctx)
	workspaceID := WorkspaceIDFromContext(ctx)
	query := `
		select c.id, c.title, c.created_at, c.updated_at, c.user_id, c.workspace_id, (
			select m.content
			from messages m
			where m.conversation_id = c.id and m.role = 'user'
			order by m.created_at asc
			limit 1
		)
		from conversations c`
	args := []any{}
	argIdx := 1
	if userID != "" {
		query += fmt.Sprintf(` where (c.user_id = $%d or c.user_id is null)`, argIdx)
		args = append(args, userID)
		argIdx++
	}
	if workspaceID != "" {
		if argIdx > 1 {
			query += ` and`
		} else {
			query += ` where`
		}
		query += fmt.Sprintf(` (c.workspace_id = $%d or c.workspace_id is null)`, argIdx)
		args = append(args, workspaceID)
		argIdx++
	}
	query += ` order by c.updated_at desc`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		var firstUserMessage *string
		var dbUserID *string
		var dbWorkspaceID *string
		if err := rows.Scan(
			&conversation.ID,
			&conversation.Title,
			&conversation.CreatedAt,
			&conversation.UpdatedAt,
			&dbUserID,
			&dbWorkspaceID,
			&firstUserMessage,
		); err != nil {
			return nil, err
		}
		if dbUserID != nil {
			conversation.UserID = *dbUserID
		}
		if dbWorkspaceID != nil {
			conversation.WorkspaceID = *dbWorkspaceID
		}
		conversation.Title = displayTitle(conversation.Title, firstUserMessage)
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

func (s *PostgresStore) CreateConversation(ctx context.Context, title string) (Conversation, error) {
	userID := UserIDFromContext(ctx)
	workspaceID := WorkspaceIDFromContext(ctx)
	id := NewID()
	if override := ConversationIDFromContext(ctx); override != "" {
		id = override
	}
	conversation := Conversation{ID: id, Title: title}
	var dbUserID *string
	var dbWorkspaceID *string
	err := s.pool.QueryRow(ctx, `
		insert into conversations (id, title, user_id, workspace_id)
		values ($1, $2, nullif($3, ''), nullif($4, ''))
		returning id, title, user_id, workspace_id, created_at, updated_at`,
		conversation.ID, conversation.Title, userID, workspaceID,
	).Scan(&conversation.ID, &conversation.Title, &dbUserID, &dbWorkspaceID, &conversation.CreatedAt, &conversation.UpdatedAt)
	if err != nil {
		return Conversation{}, err
	}
	if dbUserID != nil {
		conversation.UserID = *dbUserID
	}
	if dbWorkspaceID != nil {
		conversation.WorkspaceID = *dbWorkspaceID
	}
	return conversation, err
}

func (s *PostgresStore) UpdateConversationTitle(ctx context.Context, conversationID, title string) (Conversation, error) {
	userID := UserIDFromContext(ctx)
	var conversation Conversation
	var dbUserID *string
	query := `update conversations
		set title = $2, updated_at = now()
		where id = $1`
	args := []any{conversationID, title}
	if userID != "" {
		query += ` and (user_id = $3 or user_id is null)`
		args = append(args, userID)
	}
	query += ` returning id, title, user_id, created_at, updated_at`
	err := s.pool.QueryRow(ctx, query, args...).Scan(&conversation.ID, &conversation.Title, &dbUserID, &conversation.CreatedAt, &conversation.UpdatedAt)
	if IsNoRows(err) {
		return Conversation{}, ErrNotFound
	}
	if dbUserID != nil {
		conversation.UserID = *dbUserID
	}
	return conversation, err
}

func (s *PostgresStore) DeleteConversation(ctx context.Context, conversationID string) error {
	userID := UserIDFromContext(ctx)
	query := `delete from conversations where id = $1`
	args := []any{conversationID}
	if userID != "" {
		query += ` and (user_id = $2 or user_id is null)`
		args = append(args, userID)
	}
	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	userID := UserIDFromContext(ctx)
	rows, err := s.pool.Query(ctx, `
		select m.id, m.conversation_id, m.role, m.content, m.created_at
		from messages m
		join conversations c on c.id = m.conversation_id
		where m.conversation_id = $1
		  and ($2 = '' or c.user_id = $2 or c.user_id is null)
		order by m.created_at asc`, conversationID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, ErrNotFound
	}
	return messages, nil
}

func (s *PostgresStore) AddMessage(ctx context.Context, conversationID, role, content string) (Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)

	userID := UserIDFromContext(ctx)
	existsQuery := `select exists(select 1 from conversations where id = $1`
	existsArgs := []any{conversationID}
	if userID != "" {
		existsQuery += ` and (user_id = $2 or user_id is null)`
		existsArgs = append(existsArgs, userID)
	}
	existsQuery += `)`
	var exists bool
	if err := tx.QueryRow(ctx, existsQuery, existsArgs...).Scan(&exists); err != nil {
		return Message{}, err
	}
	if !exists {
		return Message{}, ErrNotFound
	}

	message := Message{ID: NewID(), ConversationID: conversationID, Role: role, Content: content}
	err = tx.QueryRow(ctx, `
		insert into messages (id, conversation_id, role, content)
		values ($1, $2, $3, $4)
		returning id, conversation_id, role, content, created_at`,
		message.ID, conversationID, role, content,
	).Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &message.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	if _, err := tx.Exec(ctx, `update conversations set updated_at = now() where id = $1`, conversationID); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (s *PostgresStore) ListAgentRuns(ctx context.Context) ([]AgentRun, error) {
	rows, err := s.pool.Query(ctx, `
		select id, state, summary, created_at
		from agent_runs
		order by created_at desc
		limit 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]AgentRun, 0)
	for rows.Next() {
		var run AgentRun
		if err := rows.Scan(&run.ID, &run.State, &run.Summary, &run.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *PostgresStore) AddAgentRun(ctx context.Context, state string, summary json.RawMessage) (AgentRun, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		state = "ready"
	}
	if len(summary) == 0 {
		summary = json.RawMessage(`{}`)
	}
	run := AgentRun{ID: NewID(), State: state, Summary: append(json.RawMessage(nil), summary...)}
	err := s.pool.QueryRow(ctx, `
		insert into agent_runs (id, state, summary)
		values ($1, $2, $3)
		returning id, state, summary, created_at`,
		run.ID, run.State, string(run.Summary),
	).Scan(&run.ID, &run.State, &run.Summary, &run.CreatedAt)
	return run, err
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `
		select id, email, name, created_at, updated_at
		from users
		order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *PostgresStore) SaveOAuthToken(ctx context.Context, token OAuthToken) error {
	if token.ID == "" {
		token.ID = NewID()
	}
	if token.AccessToken == nil {
		token.AccessToken = []byte{}
	}
	if token.RefreshToken == nil {
		token.RefreshToken = []byte{}
	}
	_, err := s.pool.Exec(ctx, `
		insert into oauth_tokens (id, provider, account_name, account_id, access_token, refresh_token, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (id) do update set
			provider = excluded.provider,
			account_name = excluded.account_name,
			account_id = excluded.account_id,
			access_token = excluded.access_token,
			refresh_token = excluded.refresh_token,
			expires_at = excluded.expires_at,
			updated_at = now()`,
		token.ID, token.Provider, token.AccountName, token.AccountID,
		token.AccessToken, token.RefreshToken, token.ExpiresAt,
	)
	return err
}

func (s *PostgresStore) GetOAuthToken(ctx context.Context, id string) (OAuthToken, error) {
	var token OAuthToken
	err := s.pool.QueryRow(ctx, `
		select id, provider, account_name, account_id, access_token, refresh_token, expires_at, created_at, updated_at
		from oauth_tokens where id = $1`, id,
	).Scan(&token.ID, &token.Provider, &token.AccountName, &token.AccountID,
		&token.AccessToken, &token.RefreshToken, &token.ExpiresAt, &token.CreatedAt, &token.UpdatedAt)
	if IsNoRows(err) {
		return OAuthToken{}, ErrNotFound
	}
	return token, err
}

func (s *PostgresStore) ListOAuthTokens(ctx context.Context) ([]OAuthToken, error) {
	rows, err := s.pool.Query(ctx, `
		select id, provider, account_name, account_id, access_token, refresh_token, expires_at, created_at, updated_at
		from oauth_tokens order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := make([]OAuthToken, 0)
	for rows.Next() {
		var token OAuthToken
		if err := rows.Scan(&token.ID, &token.Provider, &token.AccountName, &token.AccountID,
			&token.AccessToken, &token.RefreshToken, &token.ExpiresAt, &token.CreatedAt, &token.UpdatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (s *PostgresStore) DeleteOAuthToken(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `delete from oauth_tokens where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateUser(ctx context.Context, email, name string) (User, error) {
	user := User{ID: NewID(), Email: email, Name: name}
	err := s.pool.QueryRow(ctx, `
		insert into users (id, email, name)
		values ($1, $2, $3)
		returning id, email, name, created_at, updated_at`,
		user.ID, user.Email, user.Name,
	).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrEmailExists
		}
		return User{}, err
	}
	return user, nil
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		select id, email, name, created_at, updated_at
		from users where id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt)
	if IsNoRows(err) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		select id, email, name, created_at, updated_at
		from users where email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt)
	if IsNoRows(err) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *PostgresStore) CreateSession(ctx context.Context, userID string) (Session, error) {
	now := time.Now().UTC()
	id := NewID()
	token := NewID()
	session := Session{ID: id, UserID: userID, Token: token, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)}
	_, err := s.pool.Exec(ctx, `
		insert into sessions (id, user_id, token, expires_at)
		values ($1, $2, $3, $4)`,
		session.ID, session.UserID, session.Token, session.ExpiresAt,
	)
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *PostgresStore) GetSessionByToken(ctx context.Context, token string) (Session, error) {
	var session Session
	err := s.pool.QueryRow(ctx, `
		select id, user_id, token, created_at, expires_at
		from sessions
		where token = $1 and expires_at > now()`, token,
	).Scan(&session.ID, &session.UserID, &session.Token, &session.CreatedAt, &session.ExpiresAt)
	if IsNoRows(err) {
		return Session{}, ErrSessionNotFound
	}
	return session, err
}

func (s *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `delete from sessions where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *PostgresStore) CreateSaasAPIKey(ctx context.Context, key SaasAPIKey) (SaasAPIKey, error) {
	if key.ID == "" {
		key.ID = NewID()
	}
	err := s.pool.QueryRow(ctx, `
		insert into saas_api_keys (id, key_hash, user_id, workspace_id, name)
		values ($1, $2, $3, nullif($4, ''), $5)
		returning id, key_hash, user_id, workspace_id, name, created_at`,
		key.ID, key.KeyHash, key.UserID, key.WorkspaceID, key.Name,
	).Scan(&key.ID, &key.KeyHash, &key.UserID, &key.WorkspaceID, &key.Name, &key.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return SaasAPIKey{}, ErrKeyExists
		}
		return SaasAPIKey{}, err
	}
	return key, nil
}

func (s *PostgresStore) ListSaasAPIKeys(ctx context.Context) ([]SaasAPIKey, error) {
	rows, err := s.pool.Query(ctx, `select id, key_hash, user_id, workspace_id, name, created_at from saas_api_keys order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []SaasAPIKey
	for rows.Next() {
		var k SaasAPIKey
		if err := rows.Scan(&k.ID, &k.KeyHash, &k.UserID, &k.WorkspaceID, &k.Name, &k.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, k)
	}
	return items, rows.Err()
}

func (s *PostgresStore) DeleteSaasAPIKey(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `delete from saas_api_keys where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}
	return nil
}

func (s *PostgresStore) GetSaasAPIKeyByHash(ctx context.Context, hash string) (SaasAPIKey, error) {
	var k SaasAPIKey
	err := s.pool.QueryRow(ctx, `
		select id, key_hash, user_id, workspace_id, name, created_at
		from saas_api_keys
		where key_hash = $1`, hash,
	).Scan(&k.ID, &k.KeyHash, &k.UserID, &k.WorkspaceID, &k.Name, &k.CreatedAt)
	if IsNoRows(err) {
		return SaasAPIKey{}, ErrKeyNotFound
	}
	return k, err
}

func (s *PostgresStore) CreateWorkspace(ctx context.Context, name string) (Workspace, error) {
	id := NewID()
	w := Workspace{ID: id}
	err := s.pool.QueryRow(ctx, `
		insert into workspaces (id, name)
		values ($1, $2)
		returning id, name, created_at`,
		id, name,
	).Scan(&w.ID, &w.Name, &w.CreatedAt)
	return w, err
}

func (s *PostgresStore) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.pool.Query(ctx, `select id, name, created_at from workspaces order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return items, rows.Err()
}

func (s *PostgresStore) AddWorkspaceMember(ctx context.Context, workspaceID, userID string) error {
	_, err := s.pool.Exec(ctx, `
		insert into workspace_members (workspace_id, user_id)
		values ($1, $2)
		on conflict do nothing`, workspaceID, userID)
	return err
}

func (s *PostgresStore) RemoveWorkspaceMember(ctx context.Context, workspaceID, userID string) error {
	tag, err := s.pool.Exec(ctx, `delete from workspace_members where workspace_id = $1 and user_id = $2`, workspaceID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]WorkspaceMember, error) {
	rows, err := s.pool.Query(ctx, `
		select workspace_id, user_id, created_at
		from workspace_members
		where workspace_id = $1
		order by created_at desc`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WorkspaceMember
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.WorkspaceID, &m.UserID, &m.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (s *PostgresStore) ListBackgroundJobRecords(ctx context.Context) ([]BackgroundJobRecord, error) {
	rows, err := s.pool.Query(ctx, `
		select id, goal, mode, state, summary, max_iterations, auto_apply, created_by, workspace_id, created_at, updated_at
		from background_jobs
		order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []BackgroundJobRecord
	for rows.Next() {
		var j BackgroundJobRecord
		if err := rows.Scan(&j.ID, &j.Goal, &j.Mode, &j.State, &j.Summary, &j.MaxIterations, &j.AutoApply, &j.CreatedBy, &j.WorkspaceID, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, j)
	}
	return items, rows.Err()
}

func (s *PostgresStore) CreateBackgroundJobRecord(ctx context.Context, job BackgroundJobRecord) (BackgroundJobRecord, error) {
	if job.ID == "" {
		job.ID = NewID()
	}
	if job.State == "" {
		job.State = "pending"
	}
	if job.Mode == "" {
		job.Mode = "auto"
	}
	err := s.pool.QueryRow(ctx, `
		insert into background_jobs (id, goal, mode, state, summary, max_iterations, auto_apply, created_by, workspace_id)
		values ($1, $2, $3, $4, $5, $6, $7, nullif($8, ''), nullif($9, ''))
		returning id, goal, mode, state, summary, max_iterations, auto_apply, created_by, workspace_id, created_at, updated_at`,
		job.ID, job.Goal, job.Mode, job.State, job.Summary, job.MaxIterations, job.AutoApply, job.CreatedBy, job.WorkspaceID,
	).Scan(&job.ID, &job.Goal, &job.Mode, &job.State, &job.Summary, &job.MaxIterations, &job.AutoApply, &job.CreatedBy, &job.WorkspaceID, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

func (s *PostgresStore) UpdateBackgroundJobRecordState(ctx context.Context, id, state, summary string) error {
	tag, err := s.pool.Exec(ctx, `
		update background_jobs set state = $1, summary = $2, updated_at = now()
		where id = $3`, state, summary, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) Close() error { s.pool.Close(); return nil }

func IsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
