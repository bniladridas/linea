package store

import (
	"context"
	"encoding/json"
	"errors"
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
	query := `
		select c.id, c.title, c.created_at, c.updated_at, c.user_id, (
			select m.content
			from messages m
			where m.conversation_id = c.id and m.role = 'user'
			order by m.created_at asc
			limit 1
		)
		from conversations c`
	if userID != "" {
		query += ` where (c.user_id = $1 or c.user_id is null)`
	}
	query += ` order by c.updated_at desc`

	var rows pgx.Rows
	var err error
	if userID != "" {
		rows, err = s.pool.Query(ctx, query, userID)
	} else {
		rows, err = s.pool.Query(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		var firstUserMessage *string
		var dbUserID *string
		if err := rows.Scan(
			&conversation.ID,
			&conversation.Title,
			&conversation.CreatedAt,
			&conversation.UpdatedAt,
			&dbUserID,
			&firstUserMessage,
		); err != nil {
			return nil, err
		}
		if dbUserID != nil {
			conversation.UserID = *dbUserID
		}
		conversation.Title = displayTitle(conversation.Title, firstUserMessage)
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

func (s *PostgresStore) CreateConversation(ctx context.Context, title string) (Conversation, error) {
	userID := UserIDFromContext(ctx)
	conversation := Conversation{ID: NewID(), Title: title}
	var dbUserID *string
	err := s.pool.QueryRow(ctx, `
		insert into conversations (id, title, user_id)
		values ($1, $2, nullif($3, ''))
		returning id, title, user_id, created_at, updated_at`,
		conversation.ID, conversation.Title, userID,
	).Scan(&conversation.ID, &conversation.Title, &dbUserID, &conversation.CreatedAt, &conversation.UpdatedAt)
	if dbUserID != nil {
		conversation.UserID = *dbUserID
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

func (s *PostgresStore) Close() error { s.pool.Close(); return nil }

func IsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
