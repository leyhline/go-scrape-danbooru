# scrapedbooru – A simple command line scraper for Danbooru written in Go

I just need lots of data for playing around with machine learning.

Uses **PostgreSQL** as backend, therefore one first needs to set this up.

## Installation

First set up a go workspace, then call:
```
go get github.com/leyhline/go-scrape-danbooru
```

## Configuration

One needs to place JSON formatted config files in `$HOME/.config/scrapedbooru/`

* `database.json` containing the database configuration:
```
{
  "host":     "127.0.0.1",
  "port":     5432,
  "user":     "...",
  "password": "...",
  "database": "..."
}
```
* *(Optional)* `auth.json` for authentication with Danbooru (see the [API Documentation](https://danbooru.donmai.us/wiki_pages/43568)):
```
{
  "login": "...",
  "api_key": "..."
}
```

## Usage

At the moment it is barely usable and does not take any parameters.
