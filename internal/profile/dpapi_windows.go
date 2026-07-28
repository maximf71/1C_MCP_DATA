//go:build windows

package profile

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cryptProtectUIForbidden = 0x1

func entropy(profileID string) []byte {
	return []byte("MCP1CData/profile/v1/" + profileID)
}

func dataBlob(data []byte) windows.DataBlob {
	if len(data) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

func blobBytes(blob windows.DataBlob) []byte {
	if blob.Size == 0 || blob.Data == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(blob.Data, int(blob.Size))...)
}

func ProtectCurrentUser(plain []byte, profileID string) ([]byte, error) {
	in := dataBlob(plain)
	entropyData := entropy(profileID)
	ent := dataBlob(entropyData)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, &ent, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("DPAPI protect failed: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}

func UnprotectCurrentUser(cipher []byte, profileID string) ([]byte, error) {
	in := dataBlob(cipher)
	entropyData := entropy(profileID)
	ent := dataBlob(entropyData)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &ent, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("DPAPI decrypt failed for this Windows user: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}
