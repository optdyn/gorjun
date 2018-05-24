package raw

import (
	"net/http"
	"strings"

	uuid "github.com/satori/go.uuid"
	"github.com/subutai-io/agent/log"

	"net/url"

	"github.com/subutai-io/cdn/db"
	"github.com/subutai-io/cdn/upload"
	"fmt"
	"github.com/subutai-io/cdn/lib"
)

func Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		md5, sha256, owner := upload.Handler(w, r)
		if len(md5) == 0 || len(sha256) == 0 {
			return
		}
		info := map[string]string{
			"md5":    md5,
			"sha256": sha256,
			"type":   "raw",
		}
		r.ParseMultipartForm(32 << 20)
		if len(r.MultipartForm.Value["version"]) != 0 {
			info["version"] = r.MultipartForm.Value["version"][0]
		}
		tags := r.FormValue("tag")
		if tags == "" {
			log.Info("Can't find tag in request")
		}
		info["tag"] = tags
		_, header, _ := r.FormFile("file")
		my_uuid, _ := uuid.NewV4()
		id := my_uuid.String()
		log.Info(fmt.Sprintf("Uploading file %s with id %s and owner %s to raw repo", header.Filename, id, owner))
		db.Write(owner, id, header.Filename, info)
		if len(r.MultipartForm.Value["private"]) > 0 && r.MultipartForm.Value["private"][0] == "true" {
			log.Info("Sharing " + md5 + " with " + owner)
			db.MakePrivate(id, owner)
		} else {
			db.MakePublic(id, owner)
		}
		w.Write([]byte(id))
		log.Info(header.Filename + " saved to raw repo by " + owner)
	}
}

func Download(w http.ResponseWriter, r *http.Request) {
	uri := strings.Replace(r.RequestURI, "/kurjun/rest/file/", "/kurjun/rest/raw/", 1)
	uri = strings.Replace(uri, "/kurjun/rest/raw/get", "/kurjun/rest/raw/download", 1)
	args := strings.Split(strings.TrimPrefix(uri, "/kurjun/rest/raw/"), "/")
	if len(args) > 0 && strings.HasPrefix(args[0], "download") {
		lib.Handler("raw", w, r)
		return
	}
	if len(args) > 1 {
		parsedUrl, _ := url.Parse(uri)
		parameters, _ := url.ParseQuery(parsedUrl.RawQuery)
		var token string
		if len(parameters["token"]) > 0 {
			token = parameters["token"][0]
		}
		owner := args[0]
		file := strings.Split(args[1], "?")[0]
		if list := db.UserFile(owner, file); len(list) > 0 {
			http.Redirect(w, r, "/kurjun/rest/raw/download?id="+list[0]+"&token="+token, 302)
		}
	}
}

func Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		if len(upload.Delete(w, r)) != 0 {
			w.Write([]byte("Removed"))
			return
		}
	} else {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Bad Request"))
	}
}
