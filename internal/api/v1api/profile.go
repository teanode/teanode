package v1api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/web"
)

func setNoStoreHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "0")
}

func (self *v1Api) loadProfile() (*configs.Profile, error) {
	// Disk is the authoritative source of truth for profile settings.
	loaded, err := configs.LoadProfile()
	if err != nil {
		return nil, err
	}
	self.gateway.SetProfile(loaded)
	return loaded, nil
}

func (self *v1Api) handleProfile(writer http.ResponseWriter, request *http.Request) error {
	switch request.Method {
	case http.MethodGet:
		profile, err := self.loadProfile()
		if err != nil {
			return web.Error(500, "failed to load profile")
		}
		setNoStoreHeaders(writer)
		writer.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(writer).Encode(profile)
	case http.MethodPut:
		var body struct {
			Name          string `json:"name"`
			Bio           string `json:"bio"`
			AvatarMediaID string `json:"avatarMediaId"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return web.Error(400, "invalid request body")
		}
		existing, err := self.loadProfile()
		if err != nil {
			return web.Error(500, "failed to load profile")
		}

		profile := &configs.Profile{
			Name:          strings.TrimSpace(body.Name),
			Bio:           body.Bio,
			AvatarMediaID: strings.TrimSpace(body.AvatarMediaID),
		}
		if profile.AvatarMediaID == "" {
			profile.AvatarMediaID = strings.TrimSpace(existing.AvatarMediaID)
		}
		if err := configs.SaveProfileOverwriteBio(profile); err != nil {
			return web.Error(500, "failed to save profile")
		}
		persisted, err := self.loadProfile()
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
	profile, err := self.loadProfile()
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
		if err := configs.SaveProfile(profile); err != nil {
			return web.Error(500, "failed to save profile")
		}
		persisted, err := self.loadProfile()
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
		if err := configs.SaveProfile(profile); err != nil {
			return web.Error(500, "failed to save profile")
		}
		persisted, err := self.loadProfile()
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
