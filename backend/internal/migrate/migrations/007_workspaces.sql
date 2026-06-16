create table if not exists workspaces (
    id text primary key,
    name text not null,
    created_at timestamptz not null default now()
);

create table if not exists workspace_members (
    workspace_id text not null references workspaces(id) on delete cascade,
    user_id text not null references users(id) on delete cascade,
    created_at timestamptz not null default now(),
    primary key (workspace_id, user_id)
);

alter table conversations add column if not exists workspace_id text references workspaces(id) on delete set null;

alter table saas_api_keys add column if not exists workspace_id text references workspaces(id) on delete set null;
