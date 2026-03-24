package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func copyFile(src, dst string) {
	in, err := os.Open(src)
	must(err)
	defer in.Close()

	info, err := in.Stat()
	must(err)

	out, err := os.Create(dst)
	must(err)
	defer out.Close()

	_, err = io.Copy(out, in)
	must(err)
	must(out.Chmod(info.Mode()))
}

func createLayerTarGz(path string, fileMap map[string]string) {
	f, err := os.Create(path)
	must(err)
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for target, source := range fileMap {
		info, err := os.Stat(source)
		must(err)

		hdr, err := tar.FileInfoHeader(info, "")
		must(err)
		hdr.Name = strings.TrimPrefix(target, "/")
		hdr.ModTime = time.Unix(0, 0)
		hdr.AccessTime = time.Unix(0, 0)
		hdr.ChangeTime = time.Unix(0, 0)
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = "root"
		hdr.Gname = "root"
		must(tw.WriteHeader(hdr))

		in, err := os.Open(source)
		must(err)
		_, err = io.Copy(tw, in)
		in.Close()
		must(err)
	}
}

func fetchImage(refStr string, platform v1.Platform) v1.Image {
	ref, err := name.ParseReference(refStr)
	must(err)

	img, err := remote.Image(
		ref,
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithPlatform(platform),
	)
	must(err)
	return img
}

func saveImage(path, refStr string, img v1.Image) {
	tag, err := name.NewTag(refStr)
	must(err)
	must(tarball.WriteToFile(path, tag, img))
}

func packDirToTarGz(srcDir, destTar string) {
	out, err := os.Create(destTar)
	must(err)
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	base := filepath.Dir(srcDir)
	must(filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
		hdr.ModTime = time.Unix(0, 0)
		hdr.AccessTime = time.Unix(0, 0)
		hdr.ChangeTime = time.Unix(0, 0)
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = "root"
		hdr.Gname = "root"

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		_, err = io.Copy(tw, in)
		return err
	}))
}

func main() {
	root := "/home/ubanillx/Codes/listmonk"
	gitSHA := "25f1a8e"
	stamp := time.Now().Format("20060102-150405")
	bundleName := fmt.Sprintf("listmonk-deploy-bundle-%s", stamp)
	outRoot := filepath.Join(root, "dist", bundleName)
	imagesDir := filepath.Join(outRoot, "images")
	envDir := filepath.Join(outRoot, "env")
	scriptsDir := filepath.Join(outRoot, "scripts")
	uploadsDir := filepath.Join(outRoot, "uploads")

	must(os.MkdirAll(imagesDir, 0o755))
	must(os.MkdirAll(envDir, 0o755))
	must(os.MkdirAll(scriptsDir, 0o755))
	must(os.MkdirAll(uploadsDir, 0o755))

	platform := v1.Platform{OS: "linux", Architecture: runtime.GOARCH}
	onlineRemoteRef := "index.docker.io/listmonk/listmonk:latest"
	postgresRemoteRef := "index.docker.io/library/postgres:17-alpine"
	onlineTag := "listmonk/listmonk:latest"
	postgresTag := "postgres:17-alpine"
	localTag := fmt.Sprintf("listmonk-app:%s", gitSHA)

	fmt.Println("Fetching official listmonk image", onlineRemoteRef)
	onlineImage := fetchImage(onlineRemoteRef, platform)
	fmt.Println("Fetching postgres image", postgresRemoteRef)
	postgresImage := fetchImage(postgresRemoteRef, platform)

	layerPath := filepath.Join(os.TempDir(), fmt.Sprintf("listmonk-local-layer-%s.tar.gz", stamp))
	createLayerTarGz(layerPath, map[string]string{
		"/listmonk/listmonk": filepath.Join(root, "listmonk"),
	})
	defer os.Remove(layerPath)

	layer, err := tarball.LayerFromFile(layerPath)
	must(err)

	localImage, err := mutate.Append(onlineImage, mutate.Addendum{Layer: layer})
	must(err)

	fmt.Println("Saving image archives")
	saveImage(filepath.Join(imagesDir, "listmonk-online.tar"), onlineTag, onlineImage)
	saveImage(filepath.Join(imagesDir, "postgres.tar"), postgresTag, postgresImage)
	saveImage(filepath.Join(imagesDir, "listmonk-local.tar"), localTag, localImage)

	copyFile(filepath.Join(root, "deploy", "docker-compose.bundle.yml"), filepath.Join(outRoot, "docker-compose.bundle.yml"))
	copyFile(filepath.Join(root, "deploy", "env.runtime.example"), filepath.Join(envDir, "runtime.env.example"))
	copyFile(filepath.Join(root, "deploy", "server-load-images.sh"), filepath.Join(scriptsDir, "load-images.sh"))
	copyFile(filepath.Join(root, "deploy", "server-start-online.sh"), filepath.Join(scriptsDir, "start-online.sh"))
	copyFile(filepath.Join(root, "deploy", "server-start-local.sh"), filepath.Join(scriptsDir, "start-local.sh"))
	copyFile(filepath.Join(root, "deploy", "server-stop.sh"), filepath.Join(scriptsDir, "stop.sh"))

	must(os.WriteFile(filepath.Join(envDir, "online.env"), []byte("APP_IMAGE="+onlineTag+"\nPOSTGRES_IMAGE="+postgresTag+"\n"), 0o644))
	must(os.WriteFile(filepath.Join(envDir, "local.env"), []byte("APP_IMAGE="+localTag+"\nPOSTGRES_IMAGE="+postgresTag+"\n"), 0o644))

	readme := "# listmonk deployment bundle\n\n" +
		"1. Copy this bundle to the server.\n" +
		"2. Run: sudo ./scripts/load-images.sh\n" +
		"3. Copy env/runtime.env.example to env/runtime.env and edit it.\n" +
		"4. Start either:\n" +
		"   - sudo ./scripts/start-online.sh\n" +
		"   - sudo ./scripts/start-local.sh\n"
	must(os.WriteFile(filepath.Join(outRoot, "README.md"), []byte(readme), 0o644))

	manifest := fmt.Sprintf("{\n  \"bundle_name\": %q,\n  \"git_sha\": %q,\n  \"online_image\": %q,\n  \"local_image\": %q,\n  \"postgres_image\": %q\n}\n", bundleName, gitSHA, onlineTag, localTag, postgresTag)
	must(os.WriteFile(filepath.Join(outRoot, "manifest.json"), []byte(manifest), 0o644))

	tarPath := filepath.Join(root, "dist", bundleName+".tar.gz")
	packDirToTarGz(outRoot, tarPath)

	fmt.Println("BUNDLE_DIR=" + outRoot)
	fmt.Println("BUNDLE_TAR=" + tarPath)
}
