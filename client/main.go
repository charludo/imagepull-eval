// Copyright 2025 Edgeless Systems GmbH
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/containerd/ttrpc"
)

func Request(image, mount string) error {
	conn, err := net.Dial("unix", "/run/confidential-containers/cdh.sock")
	if err != nil {
		log.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := ttrpc.NewClient(conn)
	defer client.Close()

	imagePullerClient := NewImagePullServiceClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err = imagePullerClient.PullImage(ctx, &ImagePullRequest{ImageUrl: image, BundlePath: mount})
	return err
}
