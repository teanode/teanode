package fs

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/util/timeutil"
	"gopkg.in/yaml.v3"
)

func (self *transaction) loadConfigRecord() (*storeConfigRecord, error) {
	configRecord := &storeConfigRecord{}
	if readError := readYAMLFileOrDefault(self.configFilename(), configRecord); readError != nil {
		return nil, readError
	}
	if configRecord.Secrets == nil {
		configRecord.Secrets = map[string]string{}
	}
	return configRecord, nil
}

func (self *transaction) saveConfigRecord(configRecord *storeConfigRecord) error {
	if configRecord == nil {
		configRecord = &storeConfigRecord{}
	}
	if configRecord.Secrets == nil {
		configRecord.Secrets = map[string]string{}
	}
	return writeYAMLFile(self.configFilename(), configRecord)
}

func (self *transaction) loadSecurityRecord() (*storeSecurityRecord, error) {
	securityRecord := &storeSecurityRecord{}
	if readError := readYAMLFileOrDefault(self.securityFilename(), securityRecord); readError != nil {
		return nil, readError
	}
	if securityRecord.Users == nil {
		securityRecord.Users = map[string]storeSecurityUserRecord{}
	}
	if securityRecord.ChannelLinks.Telegram == nil {
		securityRecord.ChannelLinks.Telegram = map[string]string{}
	}
	if securityRecord.ChannelLinks.Discord == nil {
		securityRecord.ChannelLinks.Discord = map[string]string{}
	}
	normalizeSecurityUsernames(securityRecord.Users)
	return securityRecord, nil
}

func (self *transaction) saveSecurityRecord(securityRecord *storeSecurityRecord) error {
	if securityRecord == nil {
		securityRecord = &storeSecurityRecord{}
	}
	if securityRecord.Users == nil {
		securityRecord.Users = map[string]storeSecurityUserRecord{}
	}
	if securityRecord.ChannelLinks.Telegram == nil {
		securityRecord.ChannelLinks.Telegram = map[string]string{}
	}
	if securityRecord.ChannelLinks.Discord == nil {
		securityRecord.ChannelLinks.Discord = map[string]string{}
	}
	normalizeSecurityUsernames(securityRecord.Users)
	return writeYAMLFileMode(self.securityFilename(), securityRecord, 0600)
}

func (self *transaction) loadAgentRecord(agentId string) (*storeAgentRecord, error) {
	agentRecord := &storeAgentRecord{ID: agentId}
	filename := self.agentConfigFilename(agentId)
	if readError := readYAMLFileOrDefault(filename, agentRecord); readError != nil {
		return nil, readError
	}
	agentRecord.ID = agentId
	return agentRecord, nil
}

func (self *transaction) saveAgentRecord(agentId string, agentRecord *storeAgentRecord) error {
	if agentRecord == nil {
		agentRecord = &storeAgentRecord{}
	}
	agentRecord.ID = agentId
	return writeYAMLFile(self.agentConfigFilename(agentId), agentRecord)
}

func (self *transaction) listAgentRecords() ([]storeAgentRecord, error) {
	entries, readError := os.ReadDir(self.agentsDirectory())
	if readError != nil {
		if os.IsNotExist(readError) {
			return []storeAgentRecord{}, nil
		}
		return nil, readError
	}
	records := make([]storeAgentRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentId := strings.TrimSpace(entry.Name())
		if agentId == "" {
			continue
		}
		record, loadError := self.loadAgentRecord(agentId)
		if loadError != nil {
			continue
		}
		records = append(records, *record)
	}
	return records, nil
}

func (self *transaction) loadUserRecord(userId string) (*storeUserRecord, error) {
	userRecord := &storeUserRecord{ID: userId, Name: processUsername()}
	filename := self.userConfigFilename(userId)
	if readError := readYAMLFileOrDefault(filename, userRecord); readError != nil {
		return nil, readError
	}
	userRecord.ID = userId
	userRecord.Name = strings.TrimSpace(userRecord.Name)
	if userRecord.Name == "" {
		userRecord.Name = processUsername()
	}
	userRecord.Description = strings.TrimSpace(userRecord.Description)
	userRecord.AvatarMediaID = strings.TrimSpace(userRecord.AvatarMediaID)
	return userRecord, nil
}

func (self *transaction) saveUserRecord(userId string, userRecord *storeUserRecord) error {
	if userRecord == nil {
		userRecord = &storeUserRecord{}
	}
	userRecord.ID = userId
	userRecord.Name = strings.TrimSpace(userRecord.Name)
	if userRecord.Name == "" {
		userRecord.Name = processUsername()
	}
	userRecord.Description = strings.TrimSpace(userRecord.Description)
	userRecord.AvatarMediaID = strings.TrimSpace(userRecord.AvatarMediaID)
	return writeYAMLFileMode(self.userConfigFilename(userId), userRecord, 0600)
}

func (self *transaction) loadProjectRecord(projectId string) (*storeProjectRecord, error) {
	projectRecord := &storeProjectRecord{}
	filename := self.projectConfigFilename(projectId)
	fileContent, readError := os.ReadFile(filename)
	if readError != nil {
		return nil, readError
	}
	if unmarshalError := yaml.Unmarshal(fileContent, projectRecord); unmarshalError != nil {
		return nil, unmarshalError
	}
	projectRecord.ID = projectId
	return projectRecord, nil
}

func (self *transaction) saveProjectRecord(projectId string, projectRecord *storeProjectRecord) error {
	if projectRecord == nil {
		projectRecord = &storeProjectRecord{}
	}
	projectRecord.ID = projectId
	projectRecord.Name = strings.TrimSpace(projectRecord.Name)
	if projectRecord.Name == "" {
		return fmt.Errorf("name is required")
	}
	if projectRecord.UpdatedAt.IsZero() {
		projectRecord.UpdatedAt = timeutil.Now()
	}
	return writeYAMLFile(self.projectConfigFilename(projectId), projectRecord)
}

func (self *transaction) listProjectRecords() ([]storeProjectRecord, error) {
	entries, readError := os.ReadDir(self.projectsDirectory())
	if readError != nil {
		if os.IsNotExist(readError) {
			return []storeProjectRecord{}, nil
		}
		return nil, readError
	}
	projectRecords := make([]storeProjectRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectId := strings.ToLower(entry.Name())
		projectRecord, loadError := self.loadProjectRecord(projectId)
		if loadError != nil {
			continue
		}
		if strings.TrimSpace(projectRecord.Name) == "" {
			continue
		}
		projectRecords = append(projectRecords, *projectRecord)
	}
	sort.Slice(projectRecords, func(leftIndex, rightIndex int) bool {
		leftRecord := projectRecords[leftIndex]
		rightRecord := projectRecords[rightIndex]
		if leftRecord.UpdatedAt.Time.Equal(rightRecord.UpdatedAt.Time) {
			return leftRecord.Name < rightRecord.Name
		}
		return leftRecord.UpdatedAt.Time.After(rightRecord.UpdatedAt.Time)
	})
	return projectRecords, nil
}
