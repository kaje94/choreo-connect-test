# Choreo Local Bridge Service

This service bridges your local development environment with your deployed Choreo project. This bridge enables you to test and debug your local services as if they were running in the Choreo environment. The service is packaged as a container image and deployed as a Choreo BYOI service component, automatically managed by the [Choreo CLI](https://wso2.com/choreo/docs/choreo-cli/choreo-cli-overview/).

## Functionalities

- **WebSocket Connection:** Establishes real-time communication between local and Choreo environments.
- **Preview URLs:** Generates shareable preview URLs for local services.
- **Request Forwarding:** Forwards requests between local and Choreo environments.

## Connecting Local and Remote Environments

Use the following [Choreo CLI](https://wso2.com/choreo/docs/choreo-cli/choreo-cli-overview/) command to establish the bridge:

```bash
choreo connect
```

This command automatically creates the bridging service within your Choreo project, deploys it, and establishes the connection between your local and remote environments.

### Run the service locally

For local development/testing, run the bridge service with:

```shell
go run main.go
```
