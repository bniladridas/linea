create table if not exists background_jobs (
    id            text primary key,
    goal          text not null,
    mode          text not null default 'auto',
    state         text not null default 'pending',
    summary       text not null default '',
    max_iterations integer not null default 5,
    auto_apply    boolean not null default false,
    created_by    text references users(id),
    workspace_id  text references workspaces(id),
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now()
);
