package internal

import (
	"context"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

func (s *azureIaCServer) RunJob(ctx context.Context, req *pb.JobSpec) (*pb.JobHandle, error) {
	handle, err := s.provider.RunJob(ctx, jobSpecFromPB(req))
	if err != nil {
		return nil, err
	}
	return jobHandleToPB(handle), nil
}

func (s *azureIaCServer) JobStatus(ctx context.Context, req *pb.JobHandle) (*pb.JobStatusReply, error) {
	reply, err := s.provider.JobStatus(ctx, jobHandleFromPB(req))
	if err != nil {
		return nil, err
	}
	return jobStatusToPB(reply), nil
}

func (s *azureIaCServer) JobLogs(req *pb.JobHandle, stream pb.IaCProviderRunner_JobLogsServer) error {
	return s.provider.JobLogs(stream.Context(), jobHandleFromPB(req), azureRunnerLogSink{stream: stream})
}

type azureRunnerLogSink struct {
	stream pb.IaCProviderRunner_JobLogsServer
}

func (s azureRunnerLogSink) WriteLogChunk(chunk interfaces.LogChunk) error {
	return s.stream.Send(&pb.LogChunk{
		Data:   append([]byte(nil), chunk.Data...),
		Source: chunk.Source,
		Eof:    chunk.EOF,
	})
}

func jobSpecFromPB(req *pb.JobSpec) interfaces.JobSpec {
	if req == nil {
		return interfaces.JobSpec{}
	}
	return interfaces.JobSpec{
		Name:          req.GetName(),
		Kind:          req.GetKind(),
		Image:         req.GetImage(),
		RunCommand:    req.GetRunCommand(),
		EnvVars:       copyStringMap(req.GetEnvVars()),
		EnvVarsSecret: copyStringMap(req.GetEnvVarsSecret()),
		Cron:          req.GetCron(),
	}
}

func jobHandleFromPB(req *pb.JobHandle) interfaces.JobHandle {
	if req == nil {
		return interfaces.JobHandle{}
	}
	return interfaces.JobHandle{
		ID:       req.GetId(),
		Name:     req.GetName(),
		Provider: req.GetProvider(),
		Metadata: copyStringMap(req.GetMetadata()),
	}
}

func jobHandleToPB(handle *interfaces.JobHandle) *pb.JobHandle {
	if handle == nil {
		return nil
	}
	return &pb.JobHandle{
		Id:       handle.ID,
		Name:     handle.Name,
		Provider: handle.Provider,
		Metadata: copyStringMap(handle.Metadata),
	}
}

func jobStatusToPB(reply *interfaces.JobStatusReply) *pb.JobStatusReply {
	if reply == nil {
		return nil
	}
	return &pb.JobStatusReply{
		Handle:   jobHandleToPB(&reply.Handle),
		State:    jobStateToPB(reply.State),
		ExitCode: int32(reply.ExitCode),
		Message:  reply.Message,
	}
}

func jobStateToPB(state interfaces.JobState) pb.JobState {
	switch state {
	case interfaces.JobStatePending:
		return pb.JobState_JOB_STATE_PENDING
	case interfaces.JobStateRunning:
		return pb.JobState_JOB_STATE_RUNNING
	case interfaces.JobStateSucceeded:
		return pb.JobState_JOB_STATE_SUCCEEDED
	case interfaces.JobStateFailed:
		return pb.JobState_JOB_STATE_FAILED
	case interfaces.JobStateCancelled:
		return pb.JobState_JOB_STATE_CANCELLED
	default:
		return pb.JobState_JOB_STATE_UNSPECIFIED
	}
}
