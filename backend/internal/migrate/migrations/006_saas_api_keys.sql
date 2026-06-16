create table if not exists saas_api_keys (
  id text primary key,
  key_hash text not null unique,
  user_id text not null references users(id),
  name text not null default '',
  created_at timestamptz not null default now()
);
