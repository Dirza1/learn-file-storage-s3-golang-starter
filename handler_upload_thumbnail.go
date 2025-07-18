package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	fileData, fileHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, 401, "error during retrieval of data from thubnail", err)
		return
	}
	contentType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error revrieving data", err)
		return
	}
	switch contentType {
	case "image/jpeg":
		fallthrough
	case "image/png":
		break
	default:
		respondWithError(w, http.StatusBadRequest, "wrong filetupe uploded", err)
		return

	}
	immageData, err := io.ReadAll(fileData)
	if err != nil {
		respondWithError(w, 401, "error during parsing of file data to []byte", err)
		return
	}
	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "User ID of video does not match logged in user", err)
		return
	}
	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "unauthorised user", err)
	}
	var slice [32]byte
	_, err = rand.Read(slice[:])
	if err != nil {
		respondWithError(w, http.DefaultMaxHeaderBytes, "error creating byte slice", err)
		return
	}
	sliceString := base64.RawURLEncoding.EncodeToString(slice[:])
	rootString := cfg.assetsRoot
	fileExtention := strings.Split(contentType, "/")
	secondPartUrl := fmt.Sprintf("%s.%s", sliceString, fileExtention[1])
	filePathThumbNail := filepath.Join(rootString, secondPartUrl)
	file, err := os.Create(filePathThumbNail)
	if err != nil {
		respondWithError(w, http.StatusExpectationFailed, "error creating the new file", err)
		return
	}
	defer file.Close()
	sourceReader := bytes.NewReader(immageData)
	_, err = io.Copy(file, sourceReader)
	if err != nil {
		respondWithError(w, http.StatusExpectationFailed, "error copying to the new file", err)
		return
	}
	dataURL := fmt.Sprintf("http://localhost:%d/assets/%s", 8091, secondPartUrl)
	videoMetadata.ThumbnailURL = &dataURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusConflict, "error uploading new URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)
}
