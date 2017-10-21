package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"strconv"
)

const (
	configDir     = ".config/scrapedbooru"
	netloc        = "https://danbooru.donmai.us"
	netpath       = "posts.json"
	dbooruLimit   = 20
	clientTimeout = time.Second * 10
	driverName    = "postgres"
)

// Tag categories
const (
	artist    = "a"
	character = "c"
	copyright = "y"
	general   = "g"
)

type authDbooru struct {
	Login  string `json:"login"`
	ApiKey string `json:"api_key"`
}

type dbConf struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

type Post struct {
	Id                 int    `json:"id"`
	CreatedAt          string `json:"created_at"`
	UploaderId         int    `json:"uploader_id"`
	Score              int    `json:"score"`
	Source             string `json:"source"`
	Md5                string `json:"md5"`
	Rating             string `json:"rating"`
	ImageWidth         int    `json:"image_width"`
	ImageHeight        int    `json:"image_height"`
	FavCount           int    `json:"fav_count"`
	FileExt            string `json:"file_ext"`
	ParentId           int    `json:"parent_id"`
	HasChildren        bool   `json:"has_children"`
	FileSize           int    `json:"file_size"`
	FavString          string `json:"fav_string"`
	PoolString         string `json:"pool_string"`
	UpScore            int    `json:"up_score"`
	DownScore          int    `json:"down_score"`
	IsBanned           bool   `json:"is_banned"`
	PixivId            int    `json:"pixiv_id"`
	BitFlags           int    `json:"bit_flags"`
	TagStringArtist    string `json:"tag_string_artist"`
	TagStringCharacter string `json:"tag_string_character"`
	TagStringCopyright string `json:"tag_string_copyright"`
	TagStringGeneral   string `json:"tag_string_general"`
	FileUrl            string `json:"file_url"`
}

func (conf *dbConf) getUrl() string {
	return fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=disable",
		conf.User, conf.Password, conf.Host, conf.Port, conf.Database)
}

// Don't forget to call res.Body.Close()
func makeRequest(url string, client *http.Client, auth *authDbooru) *http.Response {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalln(err)
	}
	if auth != nil {
		req.SetBasicAuth(auth.Login, auth.ApiKey)
	}
	res, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	}
	if res.StatusCode != 200 {
		log.Fatalln(res.Status)
	} else if res.StatusCode == 429 {
		// TODO Maybe first wait some time and try again.
		log.Fatalln(res.Status)
	}
	return res
}

func dbInsertTags(tags string, category string, postId int, db *sql.DB) {
	// TODO Do nothing if tags are empty.
	for _, t := range strings.Split(tags, " ") {
		_, err := db.Exec("INSERT INTO tags(name, category) VALUES ($1, $2)",
			t, category)
		if err != nil {
			log.Println(err)
		}
		var tagId int
		err = db.QueryRow("SELECT id FROM tags WHERE name = $1", t).Scan(&tagId)
		if err != nil {
			log.Fatalln(err)
		}
		_, err = db.Exec("INSERT INTO tagged VALUES ($1, $2)", tagId, postId)
		if err != nil {
			log.Fatalln(err)
		}
	}
}

func dbInsert(p *Post, db *sql.DB) {
	// First insert the post itself.
	_, err := db.Exec("INSERT INTO posts VALUES "+
		"($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)",
		p.Id, p.CreatedAt, p.UploaderId, p.Score, p.Score, p.Md5, p.Rating, p.ImageWidth, p.ImageHeight,
		p.FavCount, p.FileExt, p.ParentId, p.HasChildren, p.FileSize, p.UpScore, p.DownScore,
		p.IsBanned, p.PixivId, p.BitFlags, p.FileUrl)
	if err != nil {
		log.Fatalln(err)
	}
	// Then try to insert the tags.
	dbInsertTags(p.TagStringArtist, artist, p.Id, db)
	dbInsertTags(p.TagStringCharacter, character, p.Id, db)
	dbInsertTags(p.TagStringCopyright, copyright, p.Id, db)
	dbInsertTags(p.TagStringGeneral, general, p.Id, db)
	// Insert favString.
	for _, fav := range strings.Split(p.FavString, " ") {
		userId, err := strconv.Atoi(strings.TrimPrefix(fav, "fav:"))
		if err == nil {
			_, err = db.Exec("INSERT INTO favorites VALUES ($1, $2)", userId, p.Id)
			if err != nil {
				log.Fatalln(err)
			}
		}
	}
	// Insert poolStrings.
	for _, pool := range strings.Split(p.PoolString, " ") {
		poolId, err := strconv.Atoi(strings.TrimPrefix(pool, "pool:"))
		if err == nil {
			_, err = db.Exec("INSERT INTO pools VALUES ($1, $2)", poolId, p.Id)
			if err != nil {
				log.Fatalln(err)
			}
		}
	}
}

// Save the contents of post.FileUrl in current directory.
func saveFile(post *Post, client *http.Client) {
	// TODO Check if FileUrl field exists.
	file, err := os.Create(fmt.Sprintf("%d.%s", post.Id, post.FileExt))
	if err != nil {
		log.Fatalln(err)
	}
	defer file.Close()
	res := makeRequest(netloc+post.FileUrl, client, nil)
	defer res.Body.Close()
	_, err = io.Copy(file, res.Body)
	if err != nil {
		log.Fatalln(err)
	}
}

// Parse specified JSON file from config directory
// and write contents to corresponding struct v.
func parseConfig(path string, v interface{}) {
	// TODO Return error instead of exiting.
	usr, err := user.Current()
	if err != nil {
		log.Fatalln(err)
	}
	file, err := os.Open(usr.HomeDir + "/" + configDir + "/" + path)
	if err != nil {
		log.Fatalln(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&v)
	if err != nil {
		log.Fatalln(err)
	}
}

// Query for all posts with startId <= postId < stopId.
// There is a hard limit (from the server) for a limit of 20 posts.
// Optionally use authentication if account credentials are given.
func requestPost(startId int, stopId int, client *http.Client, auth *authDbooru) []Post {
	// TODO Include post ID in logging.
	if stopId-startId > dbooruLimit {
		log.Fatalln("The hart limit for requesting posts is 20.")
	}
	query := fmt.Sprintf("?tags=id:<%d", stopId)
	url := netloc + "/" + netpath + query
	res := makeRequest(url, client, auth)
	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	var p []Post
	err := decoder.Decode(&p)
	if err != nil {
		log.Fatalln(err)
	}
	// Filter p so there are no posts with id < startId
	var pLen = len(p)
	for i := range p {
		if p[i].Id < startId {
			pLen = i
			break
		}
	}
	return p[:pLen]
}

func main() {
	client := &http.Client{
		Timeout: clientTimeout,
	}
	var auth authDbooru
	parseConfig("auth.json", &auth)
	p := requestPost(0, 20, client, &auth)
	for _, v := range p {
		fmt.Print(v.Id, " ")
	}

	var dbc dbConf
	parseConfig("database.json", &dbc)
	db, err := sql.Open(driverName, dbc.getUrl())
	if err != nil {
		log.Fatalln(err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalln(err)
	}

	for i := range p {
		dbInsert(&p[i], db)
	}
}
