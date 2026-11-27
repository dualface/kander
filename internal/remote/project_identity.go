package remote

import (
	"encoding/json"
	"path/filepath"

	"github.com/dualface/kander/v2/internal/fs"
)

// projectIdentityName is the identity marker of a client project directory:
// the server and the project the directory belongs to. Unlike the records the
// client gc removes, it stays, so `kander gc --all-projects` can still match
// the directory once they are gone. It is only a lead: the namespace must
// match and a read of the project, which the server's access control
// governs, confirms it.
const projectIdentityName = "project.json"

type projectIdentity struct {
	Schema    int    `json:"schema"`
	ServerURL string `json:"server_url"`
	ProjectID string `json:"project_id"`
}

// writeProjectIdentity writes the identity marker of the client project
// directory root unless one exists. It never rewrites a marker, also not one a
// concurrent caller created first.
func writeProjectIdentity(home, root, serverURL, projectID string) error {
	path := filepath.Join(root, projectIdentityName)
	found, err := fs.RegularFileExists(home, path)
	if err != nil || found {
		return err
	}
	data, err := json.Marshal(projectIdentity{Schema: 1, ServerURL: serverURL, ProjectID: projectID})
	if err != nil {
		return err
	}
	if err := fs.WriteTextAtomic(home, path, string(data)+"\n", false); err != nil {
		// A concurrent caller may have won the exclusive create.
		if found, statErr := fs.RegularFileExists(home, path); statErr == nil && found {
			return nil
		}
		return err
	}
	return nil
}

// readProjectIdentity reads the identity marker of the client project
// directory root. A missing marker, or one that does not decode as schema 1,
// names nothing; a marker that cannot be read is an error.
func readProjectIdentity(home, root string) (projectIdentity, bool, error) {
	var identity projectIdentity
	data, found, err := fs.ReadRegularFileIfExists(home, filepath.Join(root, projectIdentityName))
	if err != nil || !found {
		return identity, false, err
	}
	if json.Unmarshal(data, &identity) != nil || identity.Schema != 1 {
		return identity, false, nil
	}
	return identity, true, nil
}
