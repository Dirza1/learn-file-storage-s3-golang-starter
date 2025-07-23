package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	maxUploadLImit := http.MaxBytesReader(w, r.Body, 1<<30)
	r.Body = maxUploadLImit

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

	videoMeta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	if videoMeta.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "unautorised user", err)
		return
	}
	r.ParseMultipartForm(1 << 30)

	videoFile, _, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error getting video metadata", err)
		return
	}
	defer videoFile.Close()

	videoType, _, err := mime.ParseMediaType("video/mp4")
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "wrong media type", err)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during temp file creation", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	io.Copy(tempFile, videoFile)

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during reset of ofset", err)
		return
	}

	aspectRatio, err := GetVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during aspect ration gathering", err)
		return
	}
	processedVideo, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during video processing", err)
		return
	}
	processedFile, err := os.Open(processedVideo)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during opening processed file", err)
		return
	}
	defer os.Remove(processedVideo)

	bucket := cfg.s3Bucket
	key := make([]byte, 32)
	_, err = rand.Read(key)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during key generation", err)
		return
	}
	hexKey := hex.EncodeToString(key)
	s3Key := fmt.Sprintf("%s/%s.mp4", aspectRatio, hexKey)

	objImput := s3.PutObjectInput{
		Bucket:      &bucket,
		Key:         &s3Key,
		Body:        processedFile,
		ContentType: &videoType,
	}

	_, err = cfg.s3Client.PutObject(r.Context(), &objImput)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during video storage", err)
		return
	}
	videoURL := fmt.Sprintf("%s,%s", bucket, s3Key)
	videoMeta.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error during video update in database", err)
		return
	}

	videoMeta, err = cfg.dbVideoToSignedVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error generating new URL", err)
		return
	}
	respondWithJSON(w, http.StatusOK, videoMeta)
}

func GetVideoAspectRatio(filePath string) (string, error) {

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	type StreamInfo struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}

	type FFProbeOutput struct {
		Streams []StreamInfo `json:"streams"`
	}
	data := FFProbeOutput{}
	err = json.Unmarshal(buffer.Bytes(), &data)
	if err != nil {
		return "", err
	}

	var aspectRatio string
	if data.Streams[0].Height/9 == data.Streams[0].Width/16 {
		aspectRatio = "landscape"
	} else if data.Streams[0].Height/16 == data.Streams[0].Width/9 {
		aspectRatio = "portrait"
	} else {
		aspectRatio = "other"
	}

	return aspectRatio, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilepath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilepath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return outputFilepath, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	options := s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}

	s3_Client := s3.NewPresignClient(s3Client)
	v43client, err := s3_Client.PresignGetObject(context.Background(), &options, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return v43client.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	bucketKey := strings.Split(*video.VideoURL, ",")
	newURL, err := generatePresignedURL(cfg.s3Client, bucketKey[0], bucketKey[1], (5 * time.Minute))
	if err != nil {
		return database.Video{}, err
	}
	video.VideoURL = &newURL
	return video, nil
}
