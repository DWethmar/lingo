package server

import (
	"context"
	"errors"

	"github.com/dwethmar/lingo/cmd/account/app"
	"github.com/dwethmar/lingo/cmd/account/domain"
	"github.com/dwethmar/lingo/pkg/grpcerrors"
	"github.com/dwethmar/lingo/pkg/resource"
	"github.com/dwethmar/lingo/pkg/validate"
	protoaccount "github.com/dwethmar/lingo/proto/gen/go/public/account/v1"
	"github.com/google/uuid"
)

type Server struct {
	protoaccount.UnimplementedAccountServiceServer
	account        *app.Account
	resourceParser *resource.Parser
}

func New(account *app.Account, resourceParser *resource.Parser) *Server {
	return &Server{
		account:        account,
		resourceParser: resourceParser,
	}
}

func (s *Server) CreateUser(ctx context.Context, req *protoaccount.CreateUserRequest) (*protoaccount.CreateUserResponse, error) {
	if req.GetParent() == "" {
		return nil, grpcerrors.NewFieldViolationErr("parent", []grpcerrors.FieldViolation{
			{
				Field:       "parent",
				Description: "parent is required",
			},
		})
	}

	parent, err := s.resourceParser.Parse(req.GetParent())
	if err != nil {
		return nil, err
	}

	var orgID uuid.UUID
	if org := parent.Find(domain.OrganizationCollection); org != nil {
		orgID, err = org.UUID()
		if err != nil {
			return nil, err
		}
	}

	user, err := s.account.CreateUser(
		ctx,
		orgID,
		req.GetUser().GetDisplayName(),
		req.GetUser().GetEmail(),
		req.GetUser().GetPassword(),
	)
	if err != nil {
		var vErr *validate.Error
		if errors.As(err, &vErr) {
			return nil, grpcerrors.NewFieldViolationErr("validation error", []grpcerrors.FieldViolation{
				{
					Field:       vErr.Field(),
					Description: vErr.Error(),
				},
			})
		}

		return nil, err
	}

	var userout protoaccount.User
	if err = user.ToProto(&userout); err != nil {
		return nil, err
	}

	return &protoaccount.CreateUserResponse{
		User: &userout,
	}, nil
}

func (s *Server) LoginUser(ctx context.Context, req *protoaccount.LoginUserRequest) (*protoaccount.LoginUserResponse, error) {
	login, err := s.account.LoginUser(
		ctx,
		req.GetEmail(),
		req.GetPassword(),
	)
	if err != nil {
		return nil, err
	}

	var user protoaccount.User
	if err = login.User.ToProto(&user); err != nil {
		return nil, err
	}

	// grpc.SendHeader(ctx, metadata.Pairs(
	// 	"token", login.Token,
	// 	"refresh_token", login.RefreshToken,
	// ))

	return &protoaccount.LoginUserResponse{
		User:         &user,
		Token:        login.Token,
		RefreshToken: login.RefreshToken,
	}, nil
}