package storage

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

const UploadDir = "./uploads"

func EnsureUploadDirs() error {
	return os.MkdirAll(filepath.Join(UploadDir, "avatars"), 0755)
}

func ImagePath(filename string) string {
	return filepath.Join(UploadDir, filename)
}

func SaveImage(filename string, data []byte) error {
	if err := os.MkdirAll(UploadDir, 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(ImagePath(filename), data, 0644)
}

func ReadImage(filename string) ([]byte, error) {
	return ioutil.ReadFile(ImagePath(filename))
}

func RemoveImage(filename string) error {
	return os.Remove(ImagePath(filename))
}
