package main

import (
	"fmt"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rekognition"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"os"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		bucketPrefix := config.Require(ctx, "bucket_prefix")
		collectionName := config.Require(ctx, "collection_name")
		// one bucket for both input and reference images
		bucket, err := createBucket(ctx, bucketPrefix)
		if err != nil {
			return fmt.Errorf("error creating bucket: %w", err)
		}
		// uploading images
		err = uploadFiles(ctx, "../resources/images/input", bucket, "input")
		if err != nil {
			return fmt.Errorf("error uploading files: %w", err)
		}
		err = uploadFiles(ctx, "../resources/images/reference", bucket, "reference")
		if err != nil {
			return fmt.Errorf("error uploading files: %w", err)
		}

		// Creating a rekognition collection, which will contain all the users and faces
		collection, err := rekognition.NewCollection(ctx, collectionName, &rekognition.CollectionArgs{
			CollectionId: pulumi.String(collectionName),
			Tags:         nil,
			Timeouts:     nil,
		})

		if err != nil {
			return fmt.Errorf("error creating collection: %w", err)
		}

		ctx.Export("bucketName", bucket.Bucket)
		ctx.Export("collectionId", collection.CollectionId)
		return nil
	})
}

func createBucket(ctx *pulumi.Context, prefix string) (*s3.Bucket, error) {
	bucket, err := s3.NewBucket(ctx, prefix, &s3.BucketArgs{
		Acl:          pulumi.String("private"),
		ForceDestroy: pulumi.BoolPtr(true),
	})
	if err != nil {
		return nil, fmt.Errorf("error creating bucket: %w", err)
	}

	_, err = s3.NewBucketVersioningV2(ctx, "rekognition-bucket-versioning", &s3.BucketVersioningV2Args{
		Bucket: bucket.Bucket,
		VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
			Status: pulumi.String("Disabled"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error creating bucket: %w", err)
	}

	return bucket, nil
}

func uploadFiles(ctx *pulumi.Context, sourcePath string, bucket *s3.Bucket, targetPath string) error {
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return fmt.Errorf("error reading directory: %w", err)
	}

	for _, file := range entries {
		if file.Type().IsRegular() {
			_, err := s3.NewBucketObject(ctx, file.Name(), &s3.BucketObjectArgs{
				Bucket: bucket.Bucket,
				Source: pulumi.NewFileAsset(sourcePath + "/" + file.Name()),
				Key:    pulumi.String(targetPath + "/" + file.Name()),
			})

			if err != nil {
				return fmt.Errorf("error uploading file: %w", err)
			}
		}
	}

	return nil
}
