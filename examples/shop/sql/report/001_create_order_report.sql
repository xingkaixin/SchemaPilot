create table if not exists order_report (
    order_id bigint primary key,
    user_id bigint not null,
    total_cents bigint not null,
    generated_at timestamptz not null default now()
);
