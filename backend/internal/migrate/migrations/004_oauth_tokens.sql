create table if not exists oauth_tokens (
  id text primary key,
  provider text not null,
  account_name text not null default '',
  account_id text not null default '',
  access_token bytea not null,
  refresh_token bytea not null default '\x',
  expires_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists oauth_tokens_provider_idx on oauth_tokens (provider);
