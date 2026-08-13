package platform

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	maxNSSOutput  = 8 << 20
	maxNSSLine    = 16 << 10
	maxNSSUsers   = 100000
	maxNSSGroups  = 100000
	maxNSSMembers = 4096
)

type IdentityGroup struct {
	Name    string   `json:"name"`
	GID     int      `json:"gid"`
	Members []string `json:"members"`
	Source  string   `json:"source"`
	Local   bool     `json:"local"`
	Mutable bool     `json:"mutable"`
	Reason  string   `json:"reason,omitempty"`
}

type IdentityInventory struct {
	Users  []User          `json:"users"`
	Groups []IdentityGroup `json:"groups"`
}

func ListIdentityInventory(ctx context.Context) (IdentityInventory, error) {
	localUsers, err := localNames(filepath.Join("/etc", "passwd"))
	if err != nil {
		return IdentityInventory{}, err
	}
	localGroups, err := localNames(filepath.Join("/etc", "group"))
	if err != nil {
		return IdentityInventory{}, err
	}
	usersOutput, err := nssCommand(ctx, "passwd")
	if err != nil {
		return IdentityInventory{}, err
	}
	groupsOutput, err := nssCommand(ctx, "group")
	if err != nil {
		return IdentityInventory{}, err
	}
	users, err := parseNSSUsers(usersOutput, localUsers)
	if err != nil {
		return IdentityInventory{}, err
	}
	groups, err := parseNSSGroups(groupsOutput, localGroups)
	if err != nil {
		return IdentityInventory{}, err
	}
	groupNames := make(map[string][]string, len(groups))
	groupByGID := make(map[int]string, len(groups))
	for _, group := range groups {
		groupByGID[group.GID] = group.Name
		for _, member := range group.Members {
			groupNames[member] = append(groupNames[member], group.Name)
		}
	}
	for index := range users {
		users[index].Groups = append(users[index].Groups, groupNames[users[index].Username]...)
		if primary := groupByGID[users[index].GID]; primary != "" {
			users[index].Groups = append(users[index].Groups, primary)
		}
		sort.Strings(users[index].Groups)
		users[index].Groups = uniqueStrings(users[index].Groups)
	}
	return IdentityInventory{Users: users, Groups: groups}, nil
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func ListUsers(ctx context.Context) ([]User, error) {
	inventory, err := ListIdentityInventory(ctx)
	return inventory.Users, err
}

func Users() ([]User, error) {
	return ListUsers(context.Background())
}

func nssCommand(ctx context.Context, database string) (string, error) {
	command := exec.CommandContext(ctx, "getent", database)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := command.Start(); err != nil {
		return "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, maxNSSOutput+1))
	waitErr := command.Wait()
	if readErr != nil {
		return "", readErr
	}
	if len(data) > maxNSSOutput {
		return "", errors.New("NSS output exceeded bound")
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", waitErr
	}
	return string(data), nil
}

func localNames(path string) (map[string]bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := make(map[string]bool)
	scanner := bufio.NewScanner(io.LimitReader(file, maxNSSOutput))
	scanner.Buffer(make([]byte, 1024), maxNSSLine)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) > 0 && validNSSName(fields[0]) {
			result[fields[0]] = true
		}
	}
	return result, scanner.Err()
}

func parseNSSUsers(output string, local map[string]bool) ([]User, error) {
	users := make([]User, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 1024), maxNSSLine)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) != 7 || !validNSSName(fields[0]) {
			continue
		}
		uid, uidErr := strconv.Atoi(fields[2])
		gid, gidErr := strconv.Atoi(fields[3])
		if uidErr != nil || gidErr != nil || uid < 0 || gid < 0 {
			continue
		}
		isLocal := local[fields[0]]
		users = append(users, User{Username: fields[0], UID: uid, GID: gid, Name: strings.Split(fields[4], ",")[0], Home: fields[5], Shell: fields[6], System: uid < 1000, Source: sourceName(isLocal), Local: isLocal, Mutable: isLocal, Reason: remoteReason(isLocal)})
		if len(users) >= maxNSSUsers {
			return nil, errors.New("NSS user inventory exceeded bound")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(users, func(left, right int) bool { return users[left].Username < users[right].Username })
	return users, nil
}

func parseNSSGroups(output string, local map[string]bool) ([]IdentityGroup, error) {
	groups := make([]IdentityGroup, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 1024), maxNSSLine)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) != 4 || !validNSSName(fields[0]) {
			continue
		}
		gid, err := strconv.Atoi(fields[2])
		if err != nil || gid < 0 {
			continue
		}
		members := make([]string, 0)
		for _, member := range strings.Split(fields[3], ",") {
			if member != "" && validNSSName(member) {
				members = append(members, member)
			}
			if len(members) >= maxNSSMembers {
				return nil, errors.New("NSS group membership exceeded bound")
			}
		}
		isLocal := local[fields[0]]
		groups = append(groups, IdentityGroup{Name: fields[0], GID: gid, Members: members, Source: sourceName(isLocal), Local: isLocal, Mutable: isLocal, Reason: remoteReason(isLocal)})
		if len(groups) >= maxNSSGroups {
			return nil, errors.New("NSS group inventory exceeded bound")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(groups, func(left, right int) bool { return groups[left].Name < groups[right].Name })
	return groups, nil
}

func validNSSName(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n:")
}

func sourceName(local bool) string {
	if local {
		return "local"
	}
	return "nss-read-only"
}

func remoteReason(local bool) string {
	if local {
		return ""
	}
	return "Remote/NSS identity is read-only in Tako."
}
