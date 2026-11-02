// Command band-magic-id3-api serves the HTTP API in API.md.
//
//	BUCKET=<audio bucket> band-magic-id3-api   # GCS (Cloud Run)
//	band-magic-id3-api -root "/Volumes/…/Audio"     # a local folder
//
// Env: PORT (8080), ALLOWED_PREFIX (e.g. "2026/"), MAX_MB (200).
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/captainjspace/band-magic-id3/server"
)

func main() {
	root := flag.String("root", "", "serve a local folder instead of the BUCKET env bucket")
	flag.Parse()

	s := &server.Server{Prefix: os.Getenv("ALLOWED_PREFIX"), MaxSize: int64(envInt("MAX_MB", 200)) << 20}
	switch bucket := os.Getenv("BUCKET"); {
	case *root != "":
		s.Store = server.DirStore{Root: *root}
		log.Printf("serving folder %s", *root)
	case bucket != "":
		store, err := server.NewGCSStore(context.Background(), bucket)
		if err != nil {
			log.Fatal(err)
		}
		s.Store = store
		log.Printf("serving gs://%s", bucket)
	default:
		log.Fatal("set BUCKET or -root")
	}

	addr := ":" + strconv.Itoa(envInt("PORT", 8080))
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, s.Handler()))
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}
