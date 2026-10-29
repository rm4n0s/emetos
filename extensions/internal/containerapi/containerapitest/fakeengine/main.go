// Command fakeengine serves the fake Docker engine of containerapitest on a Unix socket, with a scenario of containers, images,
// networks and volumes, until it gets SIGINT or SIGTERM. The proof of step 18.2 (scripts/proof-containers.sh) points the Docker
// extension at it with the setting containers.host, so that the screenshot never shows the containers of the machine it was made
// on. It is a tool for proofs and never part of Emetos.
//
//	fakeengine -socket /tmp/ce/engine.sock -ids /tmp/ce/ids.json
//
// -ids names a file that gets the ids of the containers of the scenario as JSON ({"shop-web-1": "..."}) and the keys of the rows of
// its images ({"image:postgres:16": "i:..."}), so that a proof can run a command on one of them.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeengine:", err)
		os.Exit(1)
	}
}

// run makes the scenario and serves it until a signal.
func run() error {
	sock := flag.String("socket", "", "the Unix socket to listen on")
	ids := flag.String("ids", "", "a file that gets the ids of the containers as JSON")
	flag.Parse()
	if *sock == "" {
		flag.Usage()
		os.Exit(64)
	}
	fake, err := containerapitest.ServeUnix(containerapi.KindDocker, containerapitest.FakeOptions{}, *sock)
	if err != nil {
		return err
	}
	defer fake.Close()
	byName, err := scenario(fake)
	if err != nil {
		return err
	}
	if *ids != "" {
		data, err := json.Marshal(byName)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*ids, data, 0o600); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

// scenario fills the engine: two Compose projects and a lone container, with logs, images, a network and volumes.
func scenario(f *containerapitest.Fake) (map[string]string, error) {
	ctx := context.Background()
	c := f.Client()
	compose := func(project, service string) map[string]string {
		return map[string]string{containerapi.LabelCompose: project, containerapi.LabelComposeService: service}
	}
	f.AddImage(187_000_000, "nginx:1.27", "nginx:latest")
	f.AddImage(432_000_000, "postgres:16")
	f.AddImage(118_000_000, "ghost:5")
	f.AddImage(5_000_000, "busybox:1.37.0")
	f.AddImage(90_000_000)
	ids := map[string]string{}
	web, err := c.CreateContainer(ctx, containerapi.ContainerSpec{Name: "shop-web-1", Image: "nginx:1.27", Labels: compose("shop", "web"),
		Ports: []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 8080, ContainerPort: 80}}})
	if err != nil {
		return nil, err
	}
	if err := c.StartContainer(ctx, web.ID); err != nil {
		return nil, err
	}
	ids["shop-web-1"] = web.ID
	ids["shop-db-1"] = f.AddContainer("shop-db-1", "postgres:16", compose("shop", "db"), "running")
	ids["blog-app-1"] = f.AddContainer("blog-app-1", "ghost:5", compose("blog", "app"), "running")
	ids["blog-db-1"] = f.AddContainer("blog-db-1", "postgres:16", compose("blog", "db"), "exited")
	ids["scratch"] = f.AddContainer("scratch", "busybox:1.37.0", nil, "exited")
	for _, l := range []struct {
		stream containerapi.Stream
		text   string
	}{
		{containerapi.StreamStdout, "/docker-entrypoint.sh: Configuration complete; ready for start up"},
		{containerapi.StreamStdout, `172.18.0.1 - - [29/Oct/2026:11:58:02 +0000] "GET / HTTP/1.1" 200 615 "-" "curl/8.9.1"`},
		{containerapi.StreamStdout, `172.18.0.1 - - [29/Oct/2026:11:58:07 +0000] "GET /app.css HTTP/1.1" 200 4103 "-" "Mozilla/5.0"`},
		{containerapi.StreamStderr, "2026/10/29 11:58:09 [error] 29#29: *3 open() \"/usr/share/nginx/html/favicon.ico\" failed (2: No such file or directory)"},
		{containerapi.StreamStdout, `172.18.0.1 - - [29/Oct/2026:11:58:09 +0000] "GET /favicon.ico HTTP/1.1" 404 555 "-" "Mozilla/5.0"`},
	} {
		f.Log(web.ID, l.stream, l.text)
	}
	images, err := c.ListImages(ctx, containerapi.ListImagesOptions{})
	if err != nil {
		return nil, err
	}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			ids["image:"+tag] = "i:" + img.ID + ":" + tag // the key of the row of the image in the view
		}
	}
	if _, err := c.CreateNetwork(ctx, containerapi.NetworkSpec{Name: "shop_default"}); err != nil {
		return nil, err
	}
	if _, err := c.CreateNetwork(ctx, containerapi.NetworkSpec{Name: "blog_default"}); err != nil {
		return nil, err
	}
	for name, size := range map[string]int64{"shop_pgdata": 214_000_000, "blog_content": 38_000_000} {
		if _, err := c.CreateVolume(ctx, containerapi.VolumeSpec{Name: name}); err != nil {
			return nil, err
		}
		f.SetVolumeSize(name, size)
	}
	return ids, nil
}
