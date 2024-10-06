package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"log"
	"os"
	"regexp"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rekognition"
	"github.com/aws/aws-sdk-go-v2/service/rekognition/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type StackInfo struct {
	bucketName   string
	collectionId string
}

var stackInfo *StackInfo
var stackName *string
var existingUserIds []string

func main() {
	ctx := context.TODO()
	getFlags()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(fmt.Errorf("error loading config: %w", err))
	}
	// reading exported values from the pulumi stack
	stackInfo, err = getStackInfo(ctx)
	if err != nil {
		log.Fatal(fmt.Errorf("error getting stack info: %w", err))
	}
	log.Printf("Stack info: %v", *stackInfo)
	// preparing the AWS clients
	s3Client := s3.NewFromConfig(cfg)
	rekognitionClient := rekognition.NewFromConfig(cfg)
	// Processing the reference images
	if err := indexAndAssociateFaces(ctx, rekognitionClient, s3Client); err != nil {
		log.Fatal(err)
	}
	err = processInputImages(ctx, rekognitionClient, s3Client, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func getFlags() {
	stackName = flag.String("stack", "dev", "The name of the pulumi stack")
	flag.Parse()
}

func processInputImages(ctx context.Context, rekognitionClient *rekognition.Client, s3Client *s3.Client, images []string) error {
	images, err := getS3Objects(ctx, s3Client, stackInfo.bucketName, "input/")
	if err != nil {
		return fmt.Errorf("error getting input images: %w", err)
	}
	for _, image := range images {
		fmt.Printf("\nProcessing image: %s \n", image)
		if founds, err := searchAllFaces(ctx, rekognitionClient, image); err != nil {
			log.Fatal(err)
		} else {
			for _, found := range founds {
				fmt.Printf("Found user: %s \n", found)
			}
		}
	}
	return nil
}

func getStackInfo(ctx context.Context) (*StackInfo, error) {
	err := os.Setenv("PULUMI_CONFIG_PASSPHRASE", os.Getenv("PULUMI_CONFIG_PASSPHRASE"))
	if err != nil {
		return nil, err
	}
	var opts []auto.LocalWorkspaceOption
	opts = append(opts, auto.WorkDir("iac"))
	workspace, err := auto.NewLocalWorkspace(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("error creating workspace: %v", err)
	}
	// TODO it should be possible to select the stack from a list
	//stackSummary, err := workspace.ListStacks(ctx)
	//if err != nil {
	//	return nil, fmt.Errorf("error listing stacks: %v", err)
	//}
	//if len(stackSummary) < 1 {
	//	return nil, fmt.Errorf("no stack found")
	//}
	//stackName := stackSummary[0].Name
	stack, err := auto.SelectStack(ctx, *stackName, workspace)
	if err != nil {
		return nil, fmt.Errorf("error selecting stack: %v", err)
	}
	outputs, err := stack.Outputs(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting stack outputs: %v", err)
	}

	if outputs["bucketName"].Value == nil || outputs["collectionId"].Value == nil {
		return nil, fmt.Errorf("missing values in stack output")
	}

	return &StackInfo{
		bucketName:   outputs["bucketName"].Value.(string),
		collectionId: outputs["collectionId"].Value.(string),
	}, nil
}

func searchAllFaces(ctx context.Context, rekognitionClient *rekognition.Client, image string) ([]string, error) {
	// Gets the 10 biggest faces from the image
	users := make([]string, 0)
	faces, err := indexFacesByFilename(ctx, rekognitionClient, image, 10)
	if err != nil {
		return users, fmt.Errorf("error indexing faces: %w", err)
	}
	if len(faces.FaceRecords) < 1 {
		log.Printf("No face found on image %s", image)
	}
	for _, face := range faces.FaceRecords {
		// Find the reference face that matches the face
		result, err := rekognitionClient.SearchFaces(ctx, &rekognition.SearchFacesInput{
			CollectionId:       aws.String(stackInfo.collectionId),
			FaceId:             aws.String(*face.Face.FaceId),
			FaceMatchThreshold: aws.Float32(90),
		})
		if err != nil {
			return users, fmt.Errorf("error searching faces: %w (image: %s)", err, image)
		}
		if len(result.FaceMatches) < 1 {
			log.Printf("No match found for face %s (image: %s)", *face.Face.FaceId, image)
		} else {
			match := result.FaceMatches[0]
			// returns an array of UserID that match the FaceId or UserId , ordered by similarity score
			searchUsersOutput, err := rekognitionClient.SearchUsers(ctx, &rekognition.SearchUsersInput{
				CollectionId:       aws.String(stackInfo.collectionId),
				FaceId:             aws.String(*match.Face.FaceId),
				UserMatchThreshold: aws.Float32(75),
			})
			if err != nil {
				return users, fmt.Errorf("error searching users: %w (image: %s)", err, image)
			}
			// It should return at least one user, because the faceId is from the collection
			if len(searchUsersOutput.UserMatches) < 1 {
				log.Printf("No user found for faceId %s (image: %s)", *face.Face.FaceId, image)
			} else {
				userId := *searchUsersOutput.UserMatches[0].User.UserId
				// It handles the users that are more than once on the image :)
				if !slices.Contains(users, userId) {
					users = append(users, userId)
				}
			}
		}
	}
	return users, nil
}

func indexFacesByFilename(ctx context.Context, rekognitionClient *rekognition.Client, filename string, maxFaces int32) (*rekognition.IndexFacesOutput, error) {
	return rekognitionClient.IndexFaces(ctx, &rekognition.IndexFacesInput{
		CollectionId: aws.String(stackInfo.collectionId),
		Image: &types.Image{
			S3Object: &types.S3Object{
				Bucket: aws.String(stackInfo.bucketName),
				Name:   aws.String(filename),
			},
		},
		MaxFaces: aws.Int32(maxFaces),
		//QualityFilter: "",
	})
}

func indexAndAssociateFaces(ctx context.Context, rekognitionClient *rekognition.Client, s3Client *s3.Client) error {
	// Listing the uploaded reference images. We didn't know if an image processed yet or not.
	objects, err := getS3Objects(ctx, s3Client, stackInfo.bucketName, "reference/")
	if err != nil {
		return fmt.Errorf("error getting reference images: %w", err)
	}

	faces := make(map[string][]*types.Face)
	for _, key := range objects {
		userId, err := getUserIdFromFilename(key)
		if err != nil {
			return fmt.Errorf("error getting user id from filename %w", err)
		}
		// The biggest face on the image is indexed as a reference face
		indexFacesOutput, err := indexFacesByFilename(ctx, rekognitionClient, key, 1)
		if err != nil {
			return fmt.Errorf("error indexing faces %w", err)
		}
		if len(indexFacesOutput.FaceRecords) < 1 {
			log.Printf("No face found in image %s", key)
		} else if len(indexFacesOutput.FaceRecords) > 1 {
			log.Printf("More than one face found in image %s", key)
		}

		faces[userId] = append(faces[userId], indexFacesOutput.FaceRecords[0].Face)
	}
	// Creating the users and associating faces to them
	for userId, faceRecords := range faces {
		err := createUser(ctx, rekognitionClient, userId)
		if err != nil {
			return fmt.Errorf("error creating user %w", err)
		}
		faceIds := make([]string, 0)
		for _, face := range faceRecords {
			faceIds = append(faceIds, *face.FaceId)
		}
		_, err = rekognitionClient.AssociateFaces(ctx, &rekognition.AssociateFacesInput{
			CollectionId:       aws.String(stackInfo.collectionId),
			FaceIds:            faceIds,
			UserId:             aws.String(userId),
			UserMatchThreshold: aws.Float32(75),
		})
		if err != nil {
			return fmt.Errorf("error associating faces %w", err)
		}

	}
	return nil
}

func createUser(ctx context.Context, rekognitionClient *rekognition.Client, userId string) error {
	if existingUserIds == nil {
		users, err := rekognitionClient.ListUsers(ctx, &rekognition.ListUsersInput{
			CollectionId: aws.String(stackInfo.collectionId),
		})
		if err != nil {
			return fmt.Errorf("error requesting existing users %w", err)
		}
		existingUserIds = make([]string, 0)
		for _, user := range users.Users {
			existingUserIds = append(existingUserIds, *user.UserId)
		}
	}
	if slices.Contains(existingUserIds, userId) {
		log.Printf("User %s already exists", userId)
	} else {
		_, err := rekognitionClient.CreateUser(ctx, &rekognition.CreateUserInput{
			CollectionId: aws.String(stackInfo.collectionId),
			UserId:       aws.String(userId),
		})
		if err != nil {
			return fmt.Errorf("error creating user %w", err)
		}
		log.Printf("User %s created", userId)
	}
	return nil
}

func getUserIdFromFilename(filename string) (string, error) {
	matches := regexp.MustCompile("^reference/([a-z]+)_").FindStringSubmatch(filename)
	if len(matches) < 2 {
		return "", fmt.Errorf("error parsing filename %s", filename)
	}
	return matches[1], nil
}

func getS3Objects(ctx context.Context, s3Client *s3.Client, bucketName, prefix string) ([]string, error) {
	objects, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing s3 objects: %w", err)
	}

	keys := make([]string, 0)
	for _, object := range objects.Contents {
		keys = append(keys, *object.Key)
	}
	return keys, nil
}
