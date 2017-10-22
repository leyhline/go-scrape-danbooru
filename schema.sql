CREATE TABLE posts (
  id           integer PRIMARY KEY,
  created_at   timestamp without time zone,
  updated_at   timestamp without time zone,
  uploader_id  integer NOT NULL,
  score        integer DEFAULT 0 NOT NULL,
  source       character varying DEFAULT ''::character varying NOT NULL,
  md5          character varying NOT NULL,
  rating       character(1) DEFAULT 'q'::bpchar NOT NULL,
  image_width  integer NOT NULL,
  image_height integer NOT NULL,
  file_ext     character varying NOT NULL,
  parent_id    integer,
  has_children boolean DEFAULT false NOT NULL,
  file_size    integer NOT NULL,
  up_score     integer DEFAULT 0 NOT NULL,
  down_score   integer DEFAULT 0 NOT NULL,
  is_pending   boolean DEFAULT false NOT NULL,
  is_flagged   boolean DEFAULT false NOT NULL,
  is_deleted   boolean DEFAULT false NOT NULL,
  is_banned    boolean DEFAULT false NOT NULL,
  pixiv_id     integer,
  bit_flags    bigint DEFAULT 0 NOT NULL,
  file_url     character varying DEFAULT ''::character varying NOT NULL,
  scraped_at   timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tags (
	id         serial PRIMARY KEY,
	name       character varying NOT NULL UNIQUE,
	category   character(1) DEFAULT 'g'::bpchar NOT NULL
);

CREATE TABLE tagged (
	tag_id     integer REFERENCES tags (id),
	post_id    integer REFERENCES posts (id),
  PRIMARY KEY (tag_id, post_id)
);

CREATE TABLE pooled (
	pool_id integer,
	post_id integer REFERENCES posts (id),
  PRIMARY KEY (pool_id, post_id)
);

CREATE TABLE favorites (
	user_id integer,
	post_id integer REFERENCES posts (id),
  PRIMARY KEY (user_id, post_id)
);