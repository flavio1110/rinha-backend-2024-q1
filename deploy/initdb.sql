CREATE TABLE Accounts (
    id bigint PRIMARY KEY,
    acc_limit bigint NOT NULL,
    balance bigint NOT NULL
);

insert into
    Accounts (id, acc_limit, balance)
values
    (1, 100000, 0),
    (2, 80000, 0),
    (3, 1000000, 0),
    (4, 10000000, 0) (5, 500000, 0);

CREATE TABLE Transactions (
    id bigserial PRIMARY KEY,
    account_id bigint NOT NULL, -- no FK for simplicity and speed. Not recommended :)
    amount bigint NOT NULL,
    type varchar(1) NOT NULL, -- d or c
    description varchar(10) NOT NULL,
    created_at timestamp NOT NULL
)