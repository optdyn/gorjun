package db

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/boltdb/bolt"
	"github.com/subutai-io/base/agent/log"

	"github.com/subutai-io/gorjun/config"
)

var (
	bucket = []byte("MyBucket")
	search = []byte("SearchIndex")
	users  = []byte("Users")
	tokens = []byte("Tokens")
	authid = []byte("AuthID")
	db     = initdb()
)

func initdb() *bolt.DB {
	db, err := bolt.Open("my.db", 0600, nil)
	log.Check(log.FatalLevel, "Openning DB: my.db", err)

	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucket, search, users, tokens, authid} {
			_, err := tx.CreateBucketIfNotExists(b)
			log.Check(log.FatalLevel, "Creating bucket: "+string(b), err)
		}
		return nil
	})
	log.Check(log.FatalLevel, "Finishing update transaction", err)
	return db
}

// Temporary solution for updating db schema on production nodes
// Should be deleted when all nodes will be updated
func AlterDB() {
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		b.ForEach(func(k, v []byte) error {
			b := b.Bucket(k)
			if value := b.Get([]byte("owner")); value != nil {
				b.Delete([]byte("owner"))
				b, _ := b.CreateBucket([]byte("owner"))
				b.Put(value, []byte("w"))
			}
			return nil
		})
		return nil
	})
}

func Write(owner, key, value string, options ...map[string]string) {
	if len(owner) == 0 {
		owner = "public"
	}
	err := db.Update(func(tx *bolt.Tx) error {
		now, _ := time.Now().MarshalText()

		// Associating files with user
		b, _ := tx.Bucket(users).CreateBucketIfNotExists([]byte(owner))
		if b, err := b.CreateBucket([]byte("files")); err == nil {
			b.Put([]byte(key), []byte(value))
		}

		// Creating new record about file
		if b, err := tx.Bucket(bucket).CreateBucket([]byte(key)); err == nil {
			b.Put([]byte("date"), now)
			b.Put([]byte("name"), []byte(value))

			// Getting file size
			if f, err := os.Open(config.Filepath + key); err == nil {
				fi, _ := f.Stat()
				f.Close()
				b.Put([]byte("size"), []byte(fmt.Sprint(fi.Size())))
			}

			// Writing optional parameters for file
			for i, _ := range options {
				for k, v := range options[i] {
					b.Put([]byte(k), []byte(v))
				}
			}

			// Adding search index for files
			b, _ = tx.Bucket(search).CreateBucketIfNotExists([]byte(value))
			b.Put(now, []byte(key))
		}

		// Adding owners to files
		if b := tx.Bucket(bucket).Bucket([]byte(key)); b != nil {
			if b, _ = b.CreateBucketIfNotExists([]byte("owner")); b != nil {
				b.Put([]byte(owner), []byte("w"))
			}
		}

		return nil
	})
	log.Check(log.WarnLevel, "Writing data to db", err)
}

func Delete(owner, key string) (remains int) {
	db.Update(func(tx *bolt.Tx) error {
		var filename []byte

		// Deleting file association with user
		if b := tx.Bucket(users).Bucket([]byte(owner)); b != nil {
			if b := b.Bucket([]byte("files")); b != nil {
				filename = b.Get([]byte(key))
				b.Delete([]byte(key))
			}
		}

		// Deleting user association with file
		if b := tx.Bucket(bucket).Bucket([]byte(key)); b != nil {
			if b := b.Bucket([]byte("owner")); b != nil {
				b.Delete([]byte(owner))
				remains = b.Stats().KeyN - 1
			}
		}

		// Removing indexes and file only if no file owners left
		if remains <= 0 {
			// Deleting search index
			if b := tx.Bucket(search).Bucket([]byte(filename)); b != nil {
				b.ForEach(func(k, v []byte) error {
					if string(v) == key {
						b.Delete(k)
					}
					return nil
				})
			}

			// Removing file from DB
			tx.Bucket(bucket).DeleteBucket([]byte(key))
		}
		return nil
	})

	return
}

func Read(key string) (val string) {
	db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(bucket).Bucket([]byte(key)); b != nil {
			if value := b.Get([]byte("name")); value != nil {
				val = string(value)
			}
		}
		return nil
	})
	return val
}

func List() map[string]string {
	list := make(map[string]string)
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		b.ForEach(func(k, v []byte) error {
			if value := b.Bucket(k).Get([]byte("name")); value != nil {
				list[string(k)] = string(value)
			}
			return nil
		})
		return nil
	})
	return list
}

func Info(hash string) map[string]string {
	list := make(map[string]string)
	db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(bucket).Bucket([]byte(hash)); b != nil {
			b.ForEach(func(k, v []byte) error {
				list[string(k)] = string(v)
				return nil
			})
		}
		return nil
	})
	list["owner"] = "public"
	return list
}

func Close() {
	db.Close()
}

func Search(query string) map[string]string {
	list := make(map[string]string)
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(search)
		c := b.Cursor()
		for k, _ := c.Seek([]byte(query)); len(k) > 0 && bytes.HasPrefix(k, []byte(query)); k, _ = c.Next() {
			b.Bucket(k).ForEach(func(kk, vv []byte) error {
				list[string(vv)] = string(k)
				return nil
			})
		}
		return nil
	})
	return list
}

func LastHash(name string) (hash string) {
	db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(search).Bucket([]byte(name)); b != nil {
			_, v := b.Cursor().Last()
			hash = string(v)
		}
		return nil
	})
	return hash
}

func RegisterUser(name, key []byte) {
	db.Update(func(tx *bolt.Tx) error {
		b, err := tx.Bucket(users).CreateBucket([]byte(name))
		if !log.Check(log.WarnLevel, "Registering user "+string(name), err) {
			b.Put([]byte("key"), key)
		}
		return err
	})
}

func UserKey(name string) (key string) {
	db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(users).Bucket([]byte(name)); b != nil {
			if value := b.Get([]byte("key")); value != nil {
				key = string(value)
			}
		}
		return nil
	})
	return key
}

func SaveToken(name, token string) {
	db.Update(func(tx *bolt.Tx) error {
		if b, _ := tx.Bucket(tokens).CreateBucketIfNotExists([]byte(token)); b != nil {
			b.Put([]byte("name"), []byte(name))
			now, _ := time.Now().MarshalText()
			b.Put([]byte("date"), now)
		}
		return nil
	})
}

func CheckToken(token string) (name string) {
	hash := sha256.New()
	hash.Write([]byte(token))
	token = fmt.Sprintf("%x", hash.Sum(nil))

	db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(tokens).Bucket([]byte(token)); b != nil {
			date := new(time.Time)
			date.UnmarshalText(b.Get([]byte("date")))
			if date.Add(time.Minute * 60).Before(time.Now()) {
				return nil
			}
			if value := b.Get([]byte("name")); value != nil {
				name = string(value)
			}
		}
		return nil
	})
	return name
}

func SaveAuthID(name, token string) {
	db.Update(func(tx *bolt.Tx) error {
		tx.Bucket(authid).Put([]byte(token), []byte(name))
		return nil
	})
}

func CheckAuthID(token string) (name string) {
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(authid)
		if value := b.Get([]byte(token)); value != nil {
			name = string(value)
			b.Delete([]byte(token))
		}
		return nil
	})
	return name
}

// CheckOwner checks if user owns particular file
func CheckOwner(owner, hash string) (res bool) {
	db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(bucket).Bucket([]byte(hash)); b != nil {
			if b := b.Bucket([]byte("owner")); b != nil && b.Get([]byte(owner)) != nil {
				res = true
			}
		}
		return nil
	})
	return res
}
