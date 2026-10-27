package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/highcard-dev/daemon/internal/utils"
)

type workerDataOwner struct{ uid, gid int }

func ownershipID(value string) (int, error) {
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil || id == 1<<32-1 {
		return 0, fmt.Errorf("invalid ownership ID %q", value)
	}
	return int(id), nil
}

// Match Docker user[:group] identity inside the worker image, including named
// users. Numeric users absent from passwd default to group 0, as Docker does.
func resolveWorkerDataOwner(value string) (*workerDataOwner, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) > 2 || parts[0] == "" || len(parts) == 2 && parts[1] == "" {
		return nil, fmt.Errorf("invalid new-data-owner %q", value)
	}
	owner := &workerDataOwner{}
	uid, numericErr := ownershipID(parts[0])
	var account *user.User
	var err error
	if numericErr == nil {
		owner.uid = uid
		account, _ = user.LookupId(parts[0])
	} else {
		account, err = user.Lookup(parts[0])
		if err != nil {
			return nil, fmt.Errorf("resolve new-data-owner: %w", err)
		}
		owner.uid, err = ownershipID(account.Uid)
		if err != nil {
			return nil, err
		}
	}
	if account != nil {
		owner.gid, err = ownershipID(account.Gid)
		if err != nil {
			return nil, err
		}
	}
	if len(parts) == 2 {
		owner.gid, err = ownershipID(parts[1])
		if err != nil {
			group, lookupErr := user.LookupGroup(parts[1])
			if lookupErr != nil {
				return nil, fmt.Errorf("resolve new-data-owner group: %w", lookupErr)
			}
			owner.gid, err = ownershipID(group.Gid)
			if err != nil {
				return nil, err
			}
		}
	}
	return owner, nil
}

func applyWorkerDataOwner(root string, owner *workerDataOwner) error {
	if owner == nil {
		return nil
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// WalkDir and Lchown both avoid following symlinks outside the stage.
		return utils.SetPathOwner(path, owner.uid, owner.gid)
	})
}
