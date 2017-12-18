package utils

import (
	"encoding/json"
	"io/ioutil"
)

func JsonConfToStruct(path string, obj interface{}) error {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(file, obj); err != nil {
		return err
	}
	return nil
}
