-- drop schema
drop table attachment;
drop table email;
drop sequence attachment_seq;
drop sequence email_seq;

-- create schema
create sequence email_seq start 101;
create table email (
  id numeric not null primary key default nextval('email_seq'),
  subject varchar(1000),
  received timestamp not null,
  mailfrom varchar(1000) not null,
  mailto varchar(1000) not null,
  content text
);

create sequence attachment_seq start 101;
create table attachment (
  id numeric not null primary key default nextval('attachment_seq'),
  filename varchar(1024),
  data bytea not null,
  email numeric not null references email
);


select count(*) from email;

SELECT * FROM pg_stat_activity;

select * from email where subject like '%factuur%';

select * from email where subject like '%GMK Cherry Style Stabilizers, Keyboardbelle Cassette Futura %';

select * from attachment;

select * from email where mailfrom like '%transip%' and received > to_date('01-01-2018', 'DD-MM-YYYY') and received < to_date('31-12-2018', 'DD-MM-YYYY');