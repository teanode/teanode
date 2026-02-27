package fsstore

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/util/timeutil"
	"gopkg.in/yaml.v3"
)

func (self *fileSystemTransaction) loadConfigurationRecord() (*storeConfigurationRecord, error) {
	configurationRecord := &storeConfigurationRecord{}
	if readError := readYAMLFileOrDefault(self.configurationFilename(), configurationRecord); readError != nil {
		return nil, readError
	}
	if configurationRecord.Secrets == nil {
		configurationRecord.Secrets = map[string]string{}
	}
	return configurationRecord, nil
}

func (self *fileSystemTransaction) saveConfigurationRecord(configurationRecord *storeConfigurationRecord) error {
	if configurationRecord == nil {
		configurationRecord = &storeConfigurationRecord{}
	}
	if configurationRecord.Secrets == nil {
		configurationRecord.Secrets = map[string]string{}
	}
	return writeYAMLFile(self.configurationFilename(), configurationRecord)
}

func (self *fileSystemTransaction) loadSecurityRecord() (*storeSecurityRecord, error) {
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

func (self *fileSystemTransaction) saveSecurityRecord(securityRecord *storeSecurityRecord) error {
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

func (self *fileSystemTransaction) loadAgentRecord(agentId string) (*storeAgentRecord, error) {
	agentRecord := &storeAgentRecord{ID: agentId}
	filename := self.agentConfigurationFilename(agentId)
	if readError := readYAMLFileOrDefault(filename, agentRecord); readError != nil {
		return nil, readError
	}
	agentRecord.ID = agentId
	return agentRecord, nil
}

func (self *fileSystemTransaction) saveAgentRecord(agentId string, agentRecord *storeAgentRecord) error {
	if agentRecord == nil {
		agentRecord = &storeAgentRecord{}
	}
	agentRecord.ID = agentId
	return writeYAMLFile(self.agentConfigurationFilename(agentId), agentRecord)
}

func (self *fileSystemTransaction) listAgentRecords() ([]storeAgentRecord, error) {
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
		agentId := entry.Name()
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

func (self *fileSystemTransaction) loadUserRecord(userId string) (*storeUserRecord, error) {
	userRecord := &storeUserRecord{ID: userId, Name: processUsername()}
	filename := self.userConfigurationFilename(userId)
	if readError := readYAMLFileOrDefault(filename, userRecord); readError != nil {
		return nil, readError
	}
	userRecord.ID = userId
	userRecord.Name = strings.TrimSpace(userRecord.Name)
	if userRecord.Name == "" {
		userRecord.Name = processUsername()
	}
	userRecord.Description = strings.TrimSpace(userRecord.Description)
	return userRecord, nil
}

func (self *fileSystemTransaction) saveUserRecord(userId string, userRecord *storeUserRecord) error {
	if userRecord == nil {
		userRecord = &storeUserRecord{}
	}
	userRecord.ID = userId
	userRecord.Name = strings.TrimSpace(userRecord.Name)
	if userRecord.Name == "" {
		userRecord.Name = processUsername()
	}
	userRecord.Description = strings.TrimSpace(userRecord.Description)
	return writeYAMLFileMode(self.userConfigurationFilename(userId), userRecord, 0600)
}

func (self *fileSystemTransaction) loadProjectRecord(projectId string) (*storeProjectRecord, error) {
	projectRecord := &storeProjectRecord{}
	filename := self.projectConfigurationFilename(projectId)
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

func (self *fileSystemTransaction) saveProjectRecord(projectId string, projectRecord *storeProjectRecord) error {
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
	return writeYAMLFile(self.projectConfigurationFilename(projectId), projectRecord)
}

func (self *fileSystemTransaction) listProjectRecords() ([]storeProjectRecord, error) {
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
		if projectRecord.Name == "" {
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
