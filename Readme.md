# Rekognition Face detection PoC

This project is a proof of concept for using AWS Rekognition to manage users and detect faces.
It reads images from an S3 bucket, from two different folders: 
- `reference/` Contains images with known faces (one face per image), and user ID in the filename
- `input/` Contains images to be analyzed for faces.

## Prerequisites

- Go 1.16 or later
- Pulumi CLI
- AWS credentials configured (set as environment variables, or in `~/.aws/credentials`)


## Setup
Pulumi is used to create the S3 bucket, Rekognition collection, and upload the content of the resources/ folder to the bucket.
Thanks to this, the resources can be created and destroyed with a simple command.

```sh
cd iac
go mod tidy
pulumi up
```

#### Stack name
At the first run, it will ask the stack name you want to use. The main program accepts a stack name as an argument,
 and will read the resource identifiers from the stack output.

#### Phassphrase for the Pulumi configuration
The command will also ask a passphrase to encrypt/decrypt the Pulumi configuration.
If you provide a passphrase, it must be set as an environment variable, because the
PoC will need it to decrypt the configuration to access the IDs of the created resources.

```sh
export PULUMI_CONFIG_PASSPHRASE="your-passphrase"
```

## Running the program
The program can be run with the following commands:
```sh
go mod tidy
go run main.go -stack my-stack-name
```
If you used "dev" as your stack name, you can omit the `-stack` flag, as it is the default value.

It will index the faces from the images uploaded from the `reference/` folder of the S3 bucket and associate them with the user ID extracted from the filename.
Then, it will search for faces in the `input/` folder of the S3 bucket and print the results.

Finally, the resources can be destroyed with:
```sh
cd iac 
pulumi destroy
```

## License

This project is licensed under the MIT License.