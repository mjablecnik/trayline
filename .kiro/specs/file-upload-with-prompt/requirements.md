# Requirements Document

## Introduction

This feature adds file upload support alongside text prompts to the trayline agent API server. Users can attach files when submitting tasks (POST /run) or sending messages in chat sessions (WS /chat). Uploaded files are placed in the shared workspace directory where agent containers can access them, enabling agents to process, analyze, or transform user-provided files.

## Glossary

- **Server**: The trayline Go HTTP server that manages AI agent containers via Docker
- **Client**: The trayline Go CLI client that communicates with the Server
- **Workspace**: The shared directory mounted into all agent containers where files are stored and accessible
- **Multipart_Request**: An HTTP request using `multipart/form-data` content type that carries both structured fields and binary file data
- **Upload_Metadata**: A JSON structure describing an uploaded file including its original filename and the path where it was placed in the Workspace
- **Agent**: An AI model container (kiro or claude) that processes prompts and files

## Requirements

### Requirement 1: File Upload via REST Endpoint

**User Story:** As a user, I want to upload files alongside my prompt when submitting a one-shot task, so that the agent can process those files.

#### Acceptance Criteria

1. WHEN a POST /run request includes files in a Multipart_Request, THE Server SHALL accept the request and store the files in the Workspace before passing the prompt to the Agent
2. WHEN a POST /run request includes files, THE Server SHALL preserve the original filenames when storing files in the Workspace
3. WHEN a POST /run request includes files, THE Server SHALL place all uploaded files in a task-specific subdirectory within the Workspace to prevent filename collisions between concurrent tasks
4. WHEN a POST /run request includes no files, THE Server SHALL continue to accept the existing JSON request body format without modification
5. WHEN a POST /run request includes files but no prompt field, THE Server SHALL return a 400 error with a VALIDATION_ERROR indicating that prompt is required

### Requirement 2: File Upload via WebSocket Chat

**User Story:** As a user, I want to upload files during a chat session, so that I can provide files for the agent to work with in an interactive conversation.

#### Acceptance Criteria

1. WHEN a WebSocket client sends a binary message containing file data with metadata, THE Server SHALL store the file in the Workspace within the session-specific subdirectory
2. WHEN a file is uploaded via WebSocket, THE Server SHALL send an acknowledgment message back to the client confirming the file was stored successfully
3. WHEN a file upload via WebSocket fails, THE Server SHALL send an error message to the client with a description of the failure
4. WHEN a WebSocket client sends a text message of type "message" after uploading files, THE Agent SHALL have access to those files in the session-specific Workspace subdirectory

### Requirement 3: File Size and Count Validation

**User Story:** As an operator, I want to enforce limits on file uploads, so that the server remains stable and storage is not exhausted.

#### Acceptance Criteria

1. THE Server SHALL enforce a maximum file size of 50 MB per individual file
2. THE Server SHALL enforce a maximum of 10 files per single upload request
3. IF an uploaded file exceeds the maximum file size, THEN THE Server SHALL reject the request with a 400 error and a VALIDATION_ERROR message specifying which file exceeded the limit
4. IF the number of files in a single request exceeds the maximum count, THEN THE Server SHALL reject the request with a 400 error and a VALIDATION_ERROR message specifying the maximum allowed count
5. THE Server SHALL validate file size and count before writing any files to the Workspace

### Requirement 4: Client File Upload Support for REST

**User Story:** As a CLI user, I want to specify files to upload when running a task, so that I can provide files to the agent from my terminal.

#### Acceptance Criteria

1. WHEN the user provides one or more --file flags with the run command, THE Client SHALL include those files in a Multipart_Request to POST /run
2. WHEN the user provides --file flags, THE Client SHALL verify that each specified file exists and is readable before sending the request
3. IF a specified file does not exist or is not readable, THEN THE Client SHALL print an error message to stderr and exit with code 2
4. WHEN no --file flags are provided, THE Client SHALL send the request as a JSON body maintaining backward compatibility

### Requirement 5: Client File Upload Support for Chat

**User Story:** As a CLI user, I want to upload files during an interactive chat session, so that I can share files with the agent mid-conversation.

#### Acceptance Criteria

1. WHEN the user enters a /file command followed by a file path during a chat session, THE Client SHALL upload that file to the server via the WebSocket connection
2. WHEN a file upload acknowledgment is received from the Server, THE Client SHALL display a confirmation message to the user on stderr
3. IF the specified file does not exist or is not readable, THEN THE Client SHALL print an error message to stderr without disconnecting the session

### Requirement 6: Upload Metadata in Agent Context

**User Story:** As a user, I want the agent to know which files were uploaded and where they are located, so that the agent can reference and process them correctly.

#### Acceptance Criteria

1. WHEN files are uploaded alongside a prompt, THE Server SHALL prepend Upload_Metadata to the prompt text before passing it to the Agent, listing each file's original name and its Workspace path
2. THE Upload_Metadata format SHALL be a structured text block that the Agent can parse, containing one entry per uploaded file
3. WHEN no files are uploaded, THE Server SHALL pass the prompt to the Agent without any Upload_Metadata prefix

### Requirement 7: Workspace Cleanup

**User Story:** As an operator, I want uploaded files to be cleaned up after task completion, so that disk space is reclaimed.

#### Acceptance Criteria

1. WHEN a one-shot task reaches a terminal status (completed, failed, or cancelled), THE Server SHALL delete the task-specific subdirectory and all uploaded files within it
2. WHEN a chat session is terminated, THE Server SHALL delete the session-specific subdirectory and all uploaded files within it
3. IF file deletion fails, THEN THE Server SHALL log the error and continue normal operation without failing the task or session
