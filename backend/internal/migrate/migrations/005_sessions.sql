create table if not exists sessions (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  token text not null unique,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null
);

create index if not exists sessions_token_idx on sessions (token);

alter table conversations add column if not exists user_id text references users(id) on delete set null;
create index if not exists conversations_user_id_idx on conversations (user_id);
