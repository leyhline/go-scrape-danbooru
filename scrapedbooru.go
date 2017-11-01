/*
 * Copyright (C) 2017 Thomas Leyh
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	configDir     = ".config/scrapedbooru"
	authFilename  = "auth.json"
	dbFilename    = "database.json"
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

// Return URL to database from configuration.
func (conf *dbConf) getUrl() string {
	return fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=disable",
		conf.User, conf.Password, conf.Host, conf.Port, conf.Database)
}

type Post struct {
	Id                 int    `json:"id"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	UploaderId         int    `json:"uploader_id"`
	Score              int    `json:"score"`
	Source             string `json:"source"`
	Md5                string `json:"md5"`
	Rating             string `json:"rating"`
	ImageWidth         int    `json:"image_width"`
	ImageHeight        int    `json:"image_height"`
	FileExt            string `json:"file_ext"`
	ParentId           int    `json:"parent_id"`
	HasChildren        bool   `json:"has_children"`
	FileSize           int    `json:"file_size"`
	FavString          string `json:"fav_string"`
	PoolString         string `json:"pool_string"`
	UpScore            int    `json:"up_score"`
	DownScore          int    `json:"down_score"`
	IsPending          bool   `json:"is_pending"`
	IsFlagged          bool   `json:"is_flagged"`
	IsDeleted          bool   `json:"is_deleted"`
	IsBanned           bool   `json:"is_banned"`
	PixivId            int    `json:"pixiv_id"`
	BitFlags           int64  `json:"bit_flags"`
	TagStringArtist    string `json:"tag_string_artist"`
	TagStringCharacter string `json:"tag_string_character"`
	TagStringCopyright string `json:"tag_string_copyright"`
	TagStringGeneral   string `json:"tag_string_general"`
	FileUrl            string `json:"file_url"`
}

// Don't forget to call res.Body.Close()
func makeRequest(url string, client *http.Client, auth *authDbooru) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if auth != nil {
		req.SetBasicAuth(auth.Login, auth.ApiKey)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, errors.New("Response status indicates failure: " + res.Status)
	}
	return res, nil
}

func dbInsertTags(tags string, category string, postId int, db *sql.DB) {
	// Do nothing is there are not tags.
	if s := strings.TrimSpace(tags); s == "" {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Inserting tags failed for post: %d (%s)", postId, err)
		return
	}
	tagSplit := strings.Split(tags, " ")
	tagInsert, err := tx.Prepare("INSERT INTO tags(name, category) VALUES ($1, $2) ON CONFLICT DO NOTHING")
	if err != nil {
		log.Printf("Inserting tags failed for post: %d (%s)", postId, err)
	} else {
		for _, t := range tagSplit {
			_, err := tagInsert.Exec(t, category)
			if err != nil {
				log.Printf("Could not insert tag %s for post: %d (%s)", t, postId, err)
			}
		}
	}
	err = tx.Commit()
	if err != nil {
		log.Printf("Inserting tags failed for post: %d (%s)", postId, err)
		return
	}
	// Start new transaction for building many-to-many relationship.
	tx, err = db.Begin()
	if err != nil {
		log.Printf("Inserting tags failed for post: %d (%s)", postId, err)
		return
	}
	tagQuery, errQ := tx.Prepare("SELECT id FROM tags WHERE name = $1")
	taggedInsert, errI := tx.Prepare("INSERT INTO tagged VALUES ($1, $2) ON CONFLICT DO NOTHING")
	if errQ != nil || errI != nil {
		log.Printf("Inserting tags failed for post: %d (%s; %s)", postId, errQ, errI)
	} else {
		for _, t := range tagSplit {
			var tagId int
			err := tagQuery.QueryRow(t).Scan(&tagId)
			if err != nil {
				log.Printf("Querying tag %s failed for post: %d (%s)", t, postId, err)
			}
			_, err = taggedInsert.Exec(tagId, postId)
			if err != nil {
				log.Printf("Creating relationship for tag %d and post %d failed (%s)", tagId, postId, err)
			}
		}
	}
	err = tx.Commit()
	if err != nil {
		log.Printf("Inserting tags failed for post: %d (%s)", postId, err)
		return
	}
}

func dbInsert(p *Post, db *sql.DB) {
	// First insert the post itself.
	_, err := db.Exec("INSERT INTO posts VALUES"+
		"($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)",
		p.Id, p.CreatedAt, p.UpdatedAt, p.UploaderId, p.Score, p.Source, p.Md5, p.Rating, p.ImageWidth, p.ImageHeight,
		p.FileExt, p.ParentId, p.HasChildren, p.FileSize, p.UpScore, p.DownScore,
		p.IsPending, p.IsFlagged, p.IsDeleted, p.IsBanned, p.PixivId, p.BitFlags, p.FileUrl)
	if err != nil {
		log.Printf("Could not insert post: %d (%s)", p.Id, err)
		return
	}
	// Then try to insert the tags.
	dbInsertTags(p.TagStringArtist, artist, p.Id, db)
	dbInsertTags(p.TagStringCharacter, character, p.Id, db)
	dbInsertTags(p.TagStringCopyright, copyright, p.Id, db)
	dbInsertTags(p.TagStringGeneral, general, p.Id, db)
	if strings.TrimSpace(p.FavString) == "" &&
		strings.TrimSpace(p.PoolString) == "" {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Inserting favorites failed for post: %d (%s)", p.Id, err)
		return
	}
	stmt, err := tx.Prepare("INSERT INTO favorites VALUES ($1, $2) ON CONFLICT DO NOTHING")
	if err != nil {
		log.Printf("Inserting favorites failed for post: %d (%s)", p.Id, err)
	} else {
		for _, fav := range strings.Split(p.FavString, " ") {
			userId, err := strconv.Atoi(strings.TrimPrefix(fav, "fav:"))
			if err == nil {
				_, err = stmt.Exec(userId, p.Id)
				if err != nil {
					log.Printf("Could not insert favorite %d for post: %d (%s)", userId, p.Id, err)
				}
			}
		}
	}
	tx.Commit()
	tx, err = db.Begin()
	if err != nil {
		log.Printf("Inserting pools failed for post: %d (%s)", p.Id, err)
		return
	}
	stmt, err = tx.Prepare("INSERT INTO pooled VALUES ($1, $2) ON CONFLICT DO NOTHING")
	if err != nil {
		log.Printf("Inserting pools failed for post: %d (%s)", p.Id, err)
	} else {
		for _, pool := range strings.Split(p.PoolString, " ") {
			poolId, err := strconv.Atoi(strings.TrimPrefix(pool, "pool:"))
			if err == nil {
				_, err = stmt.Exec(poolId, p.Id)
				if err != nil {
					log.Printf("Could not insert pool %d for post: %d (%s)", poolId, p.Id, err)
				}
			}
		}
	}
	tx.Commit() // Actually commit transaction of favorites and pools.
}

// Save the contents of post.FileUrl in current directory.
func saveFile(post *Post, path string, client *http.Client) error {
	if post.FileUrl == "" {
		return errors.New("There is no FileUrl field for post: " + strconv.Itoa(post.Id))
	}
	file, err := os.Create(fmt.Sprintf("%s/%d.%s", path, post.Id, post.FileExt))
	if err != nil {
		return err
	}
	defer file.Close()
	res, err := makeRequest(netloc+post.FileUrl, client, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(file, res.Body)
	if err != nil {
		return err
	}
	return nil
}

// Parse specified JSON file from config directory
// and write contents to corresponding struct v.
func parseConfig(path string, v interface{}) error {
	usr, err := user.Current()
	if err != nil {
		return err
	}
	file, err := os.Open(usr.HomeDir + "/" + configDir + "/" + path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&v)
	if err != nil {
		return err
	}
	return nil
}

// Query for all posts with startId <= postId < stopId.
// There is a hard limit (from the server) for a limit of 20 posts.
// Optionally use authentication if account credentials are given.
// If startId == stopId then just request a single post.
func requestPost(startId int, stopId int, client *http.Client, auth *authDbooru) []Post {
	if stopId-startId > dbooruLimit {
		log.Fatalf("The hard limit for requesting posts is 20. %d posts actually requested.",
			stopId-startId)
	}
	var url string
	if startId == stopId {
		query := fmt.Sprintf("%d.json", startId)
		url = netloc + "/posts/" + query
	} else {
		query := fmt.Sprintf("?tags=id:<%d&limit=%d", stopId, dbooruLimit)
		url = netloc + "/" + netpath + query
	}
	var p []Post
	res, err := makeRequest(url, client, auth)
	if err != nil {
		log.Printf("An error occured when requesting: %s (%s)", url, err)
		return p
	}
	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	if startId == stopId {
		var post Post
		err = decoder.Decode(&post)
		p = append(p, post)
	} else {
		err = decoder.Decode(&p)
	}
	if err != nil {
		log.Printf("Failed decoding response of: %s (%s)", url, err)
		return p
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

func openDatabase(dbc *dbConf) (*sql.DB, error) {
	db, err := sql.Open(driverName, dbc.getUrl())
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, err
}

// Scrape just one batch with a maximum of 20 posts.
func scrapeBatch(startId int, stopId int, savePath string, client *http.Client, db *sql.DB, auth *authDbooru) {
	if startId > stopId {
		log.Fatalf("ERROR Invalid arguments: startId %d has to be smaller than stopId %d", startId, stopId)
	}
	ps := requestPost(startId, stopId, client, auth)
	for i := range ps {
		dbInsert(&ps[i], db)
		err := saveFile(&ps[i], savePath, client)
		if err != nil {
			log.Printf("WARNING Saving post failed: %d (%s)", ps[i].Id, err)
		}
	}
}

type intPair struct {
	first  int
	second int
}

// This is the big wrapper function called from main()
func scrapeRange(startId int, stopId int, savePath string, nrThreads int) {
	if startId > stopId {
		log.Fatalf("ERROR Invalid arguments: startId %d has to be smaller than stopId %d", startId, stopId)
	}
	// Create a client for requests.
	client := &http.Client{
		Timeout: clientTimeout,
	}
	// Read configurations.
	var auth authDbooru
	err := parseConfig(authFilename, &auth)
	if err != nil {
		log.Printf("WARNING Could not open configuration file: $HOME/%s/auth.json (%s)", configDir, err)
		log.Print("WARNING Authentication not possible. Fallback to anonymous user.")
	}
	var dbc dbConf
	err = parseConfig(dbFilename, &dbc)
	if err != nil {
		log.Fatalf("ERROR Could not open configuration file: $HOME/%s/database.json (%s)", configDir, err)
	}
	// Open database connection.
	db, err := openDatabase(&dbc)
	if err != nil {
		log.Fatalf("ERROR Could not establish database connection. (%s)", err)
	}
	// And now for the scraping itself.
	// If there is just one post don't use concurrent goroutines.
	if startId == stopId {
		scrapeBatch(startId, stopId, savePath, client, db, &auth)
		return
	}
	// <https://stackoverflow.com/questions/25306073/go-always-have-x-number-of-goroutines-running-at-any-time>
	var paramChannel = make(chan intPair)
	var waitGroup sync.WaitGroup
	// Start the specified number of goroutines.
	waitGroup.Add(nrThreads)
	for i := 0; i < nrThreads; i++ {
		go func() {
			for {
				params, ok := <-paramChannel
				if !ok {
					waitGroup.Done()
					return
				}
				scrapeBatch(params.first, params.second, savePath, client, db, &auth)
			}
		}()
	}
	// And now add all the parameter pairs to the channel, specifying the jobs.
	for currentId := startId; currentId < stopId; currentId += dbooruLimit {
		currentStop := currentId + dbooruLimit
		if currentStop > stopId {
			currentStop = stopId
		}
		paramChannel <- intPair{currentId, currentStop}
	}
	close(paramChannel)
	waitGroup.Wait()
}

func main() {
	scrapeRange(1, 1, ".", 10)
}
