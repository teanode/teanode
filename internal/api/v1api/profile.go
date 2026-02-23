package v1api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/web"
)

func setNoStoreHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "0")
}

func requestUserId(request *http.Request) string {
	if userContext := gw.UserFromContext(request.Context()); userContext != nil {
		return userContext.UserID
	}
	return ""
}

func (self *v1Api) loadProfile(userId string) (*configs.UserProfile, error) {
	return configs.LoadUserProfile(userId)
}

func (self *v1Api) handleProfile(writer http.ResponseWriter, request *http.Request) error {
	switch request.Method {
	case http.MethodGet:
		profile, err := self.loadProfile(requestUserId(request))
		if err != nil {
			return web.Error(500, "failed to load profile")
		}
		setNoStoreHeaders(writer)
		writer.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(writer).Encode(profile)
	case http.MethodPut:
		var body struct {
			Name          *string `json:"name,omitempty"`
			Description   *string `json:"description,omitempty"`
			AvatarMediaID *string `json:"avatarMediaId,omitempty"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return web.Error(400, "invalid request body")
		}
		userId := requestUserId(request)
		existing, err := self.loadProfile(userId)
		if err != nil {
			return web.Error(500, "failed to load profile")
		}

		profile := &configs.UserProfile{
			Name:          strings.TrimSpace(existing.Name),
			Description:   strings.TrimSpace(existing.Description),
			AvatarMediaID: strings.TrimSpace(existing.AvatarMediaID),
		}
		if body.Name != nil {
			profile.Name = strings.TrimSpace(*body.Name)
		}
		if body.Description != nil {
			profile.Description = strings.TrimSpace(*body.Description)
		}
		if body.AvatarMediaID != nil {
			profile.AvatarMediaID = strings.TrimSpace(*body.AvatarMediaID)
		}
		if err := configs.SaveUserProfile(userId, profile); err != nil {
			return web.Error(500, "failed to save profile")
		}
		persisted, err := self.loadProfile(userId)
		if err != nil {
			return web.Error(500, "failed to load profile")
		}

		setNoStoreHeaders(writer)
		writer.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(writer).Encode(persisted)
	default:
		return web.ErrMethodNotAllowed
	}
}

func (self *v1Api) handleProfileAvatar(writer http.ResponseWriter, request *http.Request) error {
	mediaStore := self.gateway.MediaStore()
	if mediaStore == nil {
		return web.Error(500, "media store not available")
	}
	userId := requestUserId(request)
	profile, err := self.loadProfile(userId)
	if err != nil {
		return web.Error(500, "failed to load profile")
	}

	switch request.Method {
	case http.MethodPost:
		request.Body = http.MaxBytesReader(writer, request.Body, maxAvatarUploadSize)
		if err := request.ParseMultipartForm(maxAvatarUploadSize); err != nil {
			return web.Error(400, "file too large or invalid multipart form")
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			return web.Error(400, "missing file field")
		}
		defer file.Close()

		raw, err := io.ReadAll(file)
		if err != nil {
			return web.Error(400, "failed to read file")
		}
		avatarData, format, err := processAvatarImage(raw)
		if err != nil {
			return web.Error(400, "invalid image file")
		}

		saved, err := mediaStore.Save(avatarData, format, media.SaveOptions{
			SourceType:   "profile_avatar",
			OriginalName: header.Filename,
		})
		if err != nil {
			return web.Error(500, "failed to save avatar: "+err.Error())
		}

		oldAvatarMediaId := profile.AvatarMediaID
		profile.AvatarMediaID = saved.MediaID
		if err := configs.SaveUserProfile(userId, profile); err != nil {
			return web.Error(500, "failed to save profile")
		}
		persisted, err := self.loadProfile(userId)
		if err != nil {
			return web.Error(500, "failed to load profile")
		}
		if oldAvatarMediaId != "" && oldAvatarMediaId != saved.MediaID {
			_ = mediaStore.Delete(oldAvatarMediaId)
		}

		setNoStoreHeaders(writer)
		writer.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(writer).Encode(persisted)

	case http.MethodDelete:
		oldAvatarMediaId := profile.AvatarMediaID
		profile.AvatarMediaID = ""
		if err := configs.SaveUserProfile(userId, profile); err != nil {
			return web.Error(500, "failed to save profile")
		}
		persisted, err := self.loadProfile(userId)
		if err != nil {
			return web.Error(500, "failed to load profile")
		}
		if oldAvatarMediaId != "" {
			_ = mediaStore.Delete(oldAvatarMediaId)
		}
		setNoStoreHeaders(writer)
		writer.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(writer).Encode(persisted)
	}

	return web.ErrMethodNotAllowed
}
