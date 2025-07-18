package main

import (
	"fmt"
	"io"
	"net/http"

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

	fileData, fileHeaders, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, 401, "error during retrieval of data from thubnail", err)
		return
	}
	contentType := fileHeaders.Header.Get("Content-Type")

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
	thumb := thumbnail{
		data:      immageData,
		mediaType: contentType,
	}
	videoThumbnails[videoMetadata.ID] = thumb

	url := fmt.Sprintf("http://localhost:%d/api/thumbnails/%s", 8091, videoMetadata.ID.String())

	videoMetadata.ThumbnailURL = &url

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusConflict, "error uploading new URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)
}
