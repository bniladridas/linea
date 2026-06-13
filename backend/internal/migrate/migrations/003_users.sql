create table if not exists users (
  id text primary key,
  email text not null unique,
  name text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
