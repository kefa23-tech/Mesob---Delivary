package asynqadapter

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hibiken/asynq"
	"github.com/mesob-delivery/backend/internal/dispatch/usecase"
)

type Worker struct { server *asynq.Server; mux *asynq.ServeMux }
func NewWorker(redisURL string, service *usecase.Service) (*Worker,error) {
	opt,err:=redisOptions(redisURL);if err!=nil{return nil,err};mux:=asynq.NewServeMux()
	mux.HandleFunc(FindCourierTask,func(ctx context.Context,task *asynq.Task)error{var p dispatchPayload;if err:=json.Unmarshal(task.Payload(),&p);err!=nil{return err};if p.OrderID==""||p.Tier.RadiusKM()==0{return errors.New("invalid find courier task")};return service.FindCourier(ctx,p.OrderID,p.Tier)})
	mux.HandleFunc(OfferTimeoutTask,func(ctx context.Context,task *asynq.Task)error{var p dispatchPayload;if err:=json.Unmarshal(task.Payload(),&p);err!=nil{return err};if p.OrderID==""||p.Tier.RadiusKM()==0{return errors.New("invalid courier offer timeout task")};return service.HandleOfferTimeout(ctx,p.OrderID,p.Tier)})
	return &Worker{server:asynq.NewServer(opt,asynq.Config{Concurrency:12,Queues:map[string]int{QueueName:5,"default":1}}),mux:mux},nil
}
func(w *Worker)Run()error{return w.server.Run(w.mux)}
func(w *Worker)Shutdown(){w.server.Shutdown()}
