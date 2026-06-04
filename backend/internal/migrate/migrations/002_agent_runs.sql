create table if not exists agent_runs (
  id text primary key,
  state text not null,
  summary jsonb not null,
  created_at timestamptz not null default now()
);

create index if not exists agent_runs_created_idx
  on agent_runs (created_at desc);
