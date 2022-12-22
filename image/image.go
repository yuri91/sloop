package image

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencontainers/umoci"
	"github.com/opencontainers/umoci/oci/cas/dir"
	"github.com/opencontainers/umoci/oci/casext"
	"github.com/opencontainers/umoci/oci/layer"

	"github.com/opencontainers/runtime-spec/specs-go"


	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/transports/alltransports"
)

func copy_(srcImage string, destImage string) error {

	srcRef, err := alltransports.ParseImageName(srcImage)
	if err != nil {
		return fmt.Errorf("Invalid source name %s: %v", srcImage, err)
	}
	destRef, err := alltransports.ParseImageName(destImage)
	if err != nil {
		return fmt.Errorf("Invalid destination name %s: %v", destImage, err)
	}

	policy := &signature.Policy{Default: []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()}}
	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return err
	}
	defer policyContext.Destroy()


	_, err = copy.Image(context.Background(), policyContext, destRef, srcRef, &copy.Options{
	})
	if err != nil {
		return err
	}
	return nil
}

func unpack(imagePath string, imageTag string, bundlePath string) error {
	unpackOptions  := layer.UnpackOptions {
		KeepDirlinks: true,
	}

	// Get a reference to the CAS.
	engine, err := dir.Open(imagePath)
	if err != nil {
		return err
	}
	engineExt := casext.NewEngine(engine)
	defer engine.Close()
	return umoci.Unpack(engineExt, imageTag, bundlePath, unpackOptions)
}

func Fetch(imagePath string, imageTag string, bundlePath string) error {
	tmpDir, err := os.MkdirTemp("", "sloop")
	if err != nil {
		return err
	}
	err = copy_("docker://" + imagePath + ":" + imageTag, "oci:" + tmpDir +":" + imageTag )
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	err = unpack(tmpDir, imageTag, bundlePath)
	return err
}

func Extra(bundlePath string, extraPath string, extraContent string, extraMode os.FileMode) error {
	p := filepath.Join(bundlePath, "rootfs", extraPath)
	if err := os.MkdirAll(filepath.Dir(p), 0777); err != nil {
		return err
	}
	err := os.WriteFile(p, []byte(extraContent), extraMode)
	return err
}

func ReadMetadata(bundlePath string) (*specs.Spec, error) {
	confB, err := os.ReadFile(filepath.Join(bundlePath, "config.json"))
	if err != nil {
		return nil, MetadataError.Wrap(err, "cannot read config %s", bundlePath)
	}
	var meta specs.Spec
	err = json.Unmarshal(confB, &meta)
	if err != nil {
		return nil, MetadataError.Wrap(err, "cannot unmarshal config %s", bundlePath)
	}
	return &meta, nil
}
