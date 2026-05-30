package store

import (
	"context"

	"github.com/jackc/pgx/v5"
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

func IsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
