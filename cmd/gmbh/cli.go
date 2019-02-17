package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gmbh-micro/defaults"
	"github.com/gmbh-micro/notify"
	"github.com/gmbh-micro/rpc"
	"github.com/gmbh-micro/rpc/intrigue"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func listAll() {
	client, ctx, can, err := rpc.GetControlRequest(defaults.CONTROL_HOST+defaults.CONTROL_PORT, time.Second)
	if err != nil {
		notify.LnRedF("error: " + err.Error())
	}
	defer can()

	request := intrigue.Action{
		Request: "summary.all",
	}
	reply, err := client.Summary(ctx, &request)
	if err != nil {
		notify.LnBlueF("Could not contact gmbhServer")
		notify.LnRedF("error: "+err.Error(), 1)
		return
	}
	pprintListAll(reply.GetRemotes())
}

func runReport() {
	client, ctx, can, err := rpc.GetControlRequest(defaults.CONTROL_HOST+defaults.CONTROL_PORT, time.Second)
	if err != nil {
		notify.LnBlueF("error: " + err.Error())
	}
	defer can()

	request := intrigue.Action{
		Request: "summary.all",
	}
	reply, err := client.Summary(ctx, &request)
	if err != nil {
		notify.LnBlueF("Could not contact gmbhServer")
		notify.LnRedF("error: "+err.Error(), 1)
		return
	}

	pprintListOne(reply.GetRemotes())
}

func restartAll() {
	client, ctx, can, err := rpc.GetControlRequest(defaults.CONTROL_HOST+defaults.CONTROL_PORT, time.Second)
	if err != nil {
		notify.LnRedF("error: " + err.Error())
	}
	defer can()

	request := &intrigue.Action{
		Request: "restart.all",
	}
	reply, err := client.RestartService(ctx, request)
	if err != nil {
		notify.LnRedF("error: " + err.Error())
		return
	}
	notify.LnBlueF(reply.GetMessage())
}

func listOne(id string) {
	client, ctx, can, err := rpc.GetControlRequest(defaults.CONTROL_HOST+defaults.CONTROL_PORT, time.Second*5)
	if err != nil {
		notify.LnRedF("error: " + err.Error())
	}
	defer can()

	splitID := strings.Split(id, "-")
	if len(splitID) != 2 {
		notify.LnRedF("could not parse id")
		return
	}

	request := &intrigue.Action{
		Request:  "summary.one",
		Target:   splitID[1],
		RemoteID: splitID[0],
	}
	reply, err := client.Summary(ctx, request)
	if err != nil {
		notify.LnRedF(handleErr(err))
		return
	}

	if reply.GetError() != "" {
		notify.LnRedF("could not find service with id: " + id)
		notify.LnRedF("report from core=" + reply.GetError())
		return
	}
	pprintListOne(reply.GetRemotes())
}

func restartOne(id string) {
	client, ctx, can, err := rpc.GetControlRequest(defaults.CONTROL_HOST+defaults.CONTROL_PORT, time.Second*20)
	if err != nil {
		notify.LnRedF("client error: " + err.Error())
	}
	defer can()

	splitID := strings.Split(id, "-")
	if len(splitID) != 2 {
		notify.LnRedF("could not parse id")
		return
	}

	request := &intrigue.Action{
		Request:  "restart.one",
		Target:   splitID[1],
		RemoteID: splitID[0],
	}
	reply, err := client.RestartService(ctx, request)
	if err != nil {
		fmt.Println(err)
		notify.LnRedF("send error: " + err.Error())
		return
	}

	notify.LnBlueF(reply.String())
}

func shutdown() {
	client, ctx, can, err := rpc.GetControlRequest(defaults.CONTROL_HOST+defaults.CONTROL_PORT, time.Second)
	if err != nil {
		notify.LnRedF("error: " + err.Error())
	}
	defer can()

	reply, err := client.StopServer(ctx, &intrigue.EmptyRequest{})
	if err != nil {
		notify.LnRedF("error: " + err.Error())
		return
	}
	notify.LnBlueF(reply.String())
}

func handleErr(err error) string {
	if grpc.Code(err) == codes.Unavailable {
		return "could not connect to gmbhCore"
	}
	return "unsupported error code"
}
