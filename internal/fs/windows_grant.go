//go:build windows

package fs

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// localGroupReadAccess is read-and-execute: read data, attributes, extended attributes and the security
// descriptor, plus list and traverse on directories. It grants no write, delete, or permission change.
const localGroupReadAccess = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE

// securityNTNonUnique is SECURITY_NT_NON_UNIQUE, the first sub-authority of account domain SIDs.
const securityNTNonUnique = 21

func (d *windowsTempDir) grantLocalGroupRead(groups []string) error {
	if d == nil || d.handle == 0 {
		return nil
	}
	sids, err := resolveLocalGroups(groups)
	if err != nil {
		return err
	}
	if len(sids) == 0 {
		return nil
	}
	if err := validateHandleKind(d.handle, d.path, kindDirectory); err != nil {
		return err
	}
	return grantReadWin(d.handle, d.path, sids, true, 0, new(int))
}

// resolveLocalGroups maps group names to SIDs of this machine's account domain. A name that does not
// resolve is skipped; a name that resolves to anything other than a local group of this machine fails.
func resolveLocalGroups(groups []string) ([]*windows.SID, error) {
	computer, err := windows.ComputerName()
	if err != nil {
		return nil, wrap("acl", "", err)
	}
	var sids []*windows.SID
	for _, group := range groups {
		sid, ok, err := resolveLocalGroup(computer, group)
		if err != nil {
			return nil, err
		}
		if ok {
			sids = append(sids, sid)
		}
	}
	return sids, nil
}

func resolveLocalGroup(computer, group string) (*windows.SID, bool, error) {
	if group == "" || strings.ContainsAny(group, `\/@`) {
		return nil, false, failClosed("acl", group, "review read group must be a plain local group name")
	}
	// The plain name is looked up so that builtin and well-known groups resolve and are rejected below instead of
	// being skipped as unknown; the local account domain is searched before any primary domain.
	sid, domain, accountType, err := windows.LookupSID("", group)
	if err != nil {
		if errors.Is(err, windows.ERROR_NONE_MAPPED) {
			return nil, false, nil
		}
		return nil, false, wrap("acl", group, err)
	}
	if accountType != windows.SidTypeAlias || !strings.EqualFold(domain, computer) || !isMachineDomainSID(sid) {
		return nil, false, failClosed("acl", group, "review read group is not a local group of this machine's account domain")
	}
	return sid, true, nil
}

// isMachineDomainSID accepts S-1-5-21-a-b-c-rid: an account of a local account domain. Builtin groups
// (S-1-5-32-*) and well-known SIDs such as Everyone or Authenticated Users have a different shape.
func isMachineDomainSID(sid *windows.SID) bool {
	if sid == nil || !sid.IsValid() {
		return false
	}
	if sid.IdentifierAuthority() != windows.SECURITY_NT_AUTHORITY {
		return false
	}
	return sid.SubAuthorityCount() == 5 && sid.SubAuthority(0) == securityNTNonUnique
}

func grantReadWin(handle windows.Handle, path string, sids []*windows.SID, isDir bool, depth int, seen *int) error {
	if err := addReadEntries(handle, path, sids, isDir); err != nil {
		return err
	}
	if !isDir {
		return nil
	}
	if depth > tempCleanupMaxDepth {
		return failClosed("acl", path, "private temporary directory depth exceeded")
	}
	remaining := tempCleanupMaxEntries - *seen
	if remaining < 0 {
		return failClosed("acl", path, "private temporary directory entry budget exceeded")
	}
	names, err := listNames(handle, path, remaining)
	if err != nil {
		return err
	}
	*seen += len(names)
	for _, name := range names {
		child := filepath.Join(path, name)
		probe, err := tryOpenLeaf(handle, name, child, windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
		if err != nil {
			return err
		}
		if probe == 0 {
			continue
		}
		info, err := attributeInfo(probe, child)
		closeHandle(probe)
		if err != nil {
			return err
		}
		childIsDir := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
		access := uint32(windows.READ_CONTROL | windows.WRITE_DAC | windows.FILE_READ_ATTRIBUTES)
		expected := kindFile
		if childIsDir {
			access |= windows.FILE_LIST_DIRECTORY
			expected = kindDirectory
		}
		h, err := tryOpenLeaf(handle, name, child, access, expected, true, false)
		if err != nil {
			return err
		}
		if h == 0 {
			continue
		}
		err = grantReadWin(h, child, sids, childIsDir, depth+1, seen)
		closeHandle(h)
		if err != nil {
			return err
		}
	}
	return nil
}

// addReadEntries merges one read entry per SID into the object's DACL and keeps the DACL's protection state.
func addReadEntries(handle windows.Handle, path string, sids []*windows.SID, isDir bool) error {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return wrap("acl", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return wrap("acl", path, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		return wrap("acl", path, err)
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if isDir {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(sids))
	for _, sid := range sids {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: localGroupReadAccess,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_ALIAS,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	merged, err := windows.ACLFromEntries(entries, dacl)
	if err != nil {
		return wrap("acl", path, err)
	}
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		info |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		info |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, info, nil, nil, merged, nil); err != nil {
		return wrap("acl", path, err)
	}
	return nil
}
