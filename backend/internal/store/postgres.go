package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	rows, err := s.pool.Query(ctx, `
		select c.id, c.title, c.created_at, c.updated_at, (
			select m.content
			from messages m
			where m.conversation_id = c.id and m.role = 'user'
			order by m.created_at asc
			limit 1
		)
		from conversations c
		order by c.updated_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		var firstUserMessage *string
		if err := rows.Scan(
			&conversation.ID,
			&conversation.Title,
			&conversation.CreatedAt,
			&conversation.UpdatedAt,
			&firstUserMessage,
		); err != nil {
			return nil, err
		}
		conversation.Title = displayTitle(conversation.Title, firstUserMessage)
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

func (s *PostgresStore) CreateConversation(ctx context.Context, title string) (Conversation, error) {
	conversation := Conversation{ID: NewID(), Title: title}
	err := s.pool.QueryRow(ctx, `
		insert into conversations (id, title)
		values ($1, $2)
		returning id, title, created_at, updated_at`,
		conversation.ID, conversation.Title,
	).Scan(&conversation.ID, &conversation.Title, &conversation.CreatedAt, &conversation.UpdatedAt)
	return conversation, err
}

func (s *PostgresStore) UpdateConversationTitle(ctx context.Context, conversationID, title string) (Conversation, error) {
	var conversation Conversation
	err := s.pool.QueryRow(ctx, `
		update conversations
		set title = $2, updated_at = now()
		where id = $1
		returning id, title, created_at, updated_at`,
		conversationID, title,
	).Scan(&conversation.ID, &conversation.Title, &conversation.CreatedAt, &conversation.UpdatedAt)
	if IsNoRows(err) {
		return Conversation{}, ErrNotFound
	}
	return conversation, err
}

func (s *PostgresStore) DeleteConversation(ctx context.Context, conversationID string) error {
	tag, err := s.pool.Exec(ctx, `delete from conversations where id = $1`, conversationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := s.pool.Query(ctx, `
		select id, conversation_id, role, content, created_at
		from messages
		where conversation_id = $1
		order by created_at asc`, conversationID)
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
		var exists bool
		err := s.pool.QueryRow(ctx, `select exists(select 1 from conversations where id = $1)`, conversationID).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	return messages, nil
}

func (s *PostgresStore) AddMessage(ctx context.Context, conversationID, role, content string) (Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from conversations where id = $1)`, conversationID).Scan(&exists); err != nil {
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

func (s *PostgresStore) Close() error { s.pool.Close(); return nil }

func IsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
