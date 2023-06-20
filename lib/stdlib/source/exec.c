#include "ddptypes.h"
#include "ddpos.h"
#include "ddpmemory.h"
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

#if DDPOS_WINDOWS
#include "ddpwindows.h"
#else // DDPOS_LINUX
#include <unistd.h>
#include <sys/wait.h>
#include <sys/types.h>
#endif // DDPOS_WINDOWS

#define READ_END 0
#define WRITE_END 1
#define BUFF_SIZE 512

#if DDPOS_WINDOWS
// creates a pipe and sets the inherit_handle to be inherited
static bool create_pipe(HANDLE pipe_handles[], int inherit_handle) {
	return CreatePipe(&pipe_handles[READ_END], &pipe_handles[WRITE_END], NULL, 0) &&
		SetHandleInformation(pipe_handles[inherit_handle], HANDLE_FLAG_INHERIT, HANDLE_FLAG_INHERIT);
}

static void close_pipe(HANDLE pipe_handles[]) {
	CloseHandle(pipe_handles[0]);
	CloseHandle(pipe_handles[1]);
}

// reads everything from the given pipe into out
// then closes the pipe
// returns the new size of out
//
// TODO: read in binary mode to not have annoying \r s
static size_t read_pipe(HANDLE handle, char** out) {
	char buff[BUFF_SIZE];
	size_t out_size = 0;
	DWORD nread = 0;
	while (ReadFile(handle, buff, BUFF_SIZE, &nread, 0)) {
		*out = ddp_reallocate(*out, out_size, out_size + nread);
		memcpy(*out + out_size, buff, nread);
		out_size += nread;
	}
    CloseHandle(handle);
	return out_size;
}

static ddpint execute_process(ddpstring* path, ddpstringlist* args,
    ddpstring* input, ddpstringref stdoutput, ddpstringref erroutput)
{
	HANDLE stdout_pipe[2];
    HANDLE stderr_pipe[2];
    HANDLE stdin_pipe[2];

    const bool need_stderr = stdoutput != erroutput;

	// for stdout and stderr we want to inherit the write end to the child, as it writes to those pipes
	// for stdin we inherit the read end because the child because it reads from it
	if (!create_pipe(stdout_pipe, WRITE_END)) {
		return -1;
	}
	if (need_stderr && !create_pipe(stderr_pipe, WRITE_END)) {
		close_pipe(stdout_pipe);
		return -1;
	}
	if (!create_pipe(stdin_pipe, READ_END)) {
		close_pipe(stdout_pipe);
		if (need_stderr) {
			close_pipe(stderr_pipe);
		}
		return -1;
	}
	
	// prepare the arguments
	char* argv;
	size_t argv_size = strlen(path->str) + 1;
	for (ddpint i = 0; i < args->len; i++) {
		argv_size += args->arr[i].cap; // the nullterminator is used for the trailing space
	}
	argv = ALLOCATE(char, argv_size);
	argv[0] = '\0'; // make sure strcat works
	strcat(argv, path->str);
	strcat(argv, " ");
	for (ddpint i = 0; i < args->len; i++) {
		strcat(argv, args->arr[i].str);
		if (i < args->len-1) {
			strcat(argv, " ");
		}
	}

	// prepare the output
    FREE_ARRAY(char, stdoutput->str, stdoutput->cap);
    stdoutput->cap = 0;
    stdoutput->str = NULL;
    if (need_stderr) {
        FREE_ARRAY(char, erroutput->str, erroutput->cap);
        erroutput->cap = 0;
        erroutput->str = NULL;
    }

	// setup what to inherit for the child process
	STARTUPINFOA si = {0};
    si.cb = sizeof(si);
    si.hStdInput = stdin_pipe[READ_END];
    si.hStdOutput = stdout_pipe[WRITE_END];
    si.hStdError = need_stderr ? stderr_pipe[WRITE_END] : stdout_pipe[WRITE_END];
    si.dwFlags |= STARTF_USESTDHANDLES;

	// start the actual child process
	PROCESS_INFORMATION pi;
	if (!CreateProcessA(path->str, argv, NULL, NULL, true, 0, NULL, NULL, &si, &pi)) {
		close_pipe(stdout_pipe);
		if (need_stderr) {
			close_pipe(stderr_pipe);
		}
		close_pipe(stdin_pipe);
		FREE_ARRAY(char, argv, argv_size); // free the arguments
		return -1;
	}
	FREE_ARRAY(char, argv, argv_size); // free the arguments

	// you NEED to close these, or it will not work
	CloseHandle(pi.hThread);
	CloseHandle(stdout_pipe[WRITE_END]);
	if (need_stderr) {
		CloseHandle(stderr_pipe[WRITE_END]);
	}
	CloseHandle(stdin_pipe[READ_END]);


	// write stdin
    DWORD len_written = 0;
	DWORD len_to_write = strlen(input->str);
    if (!WriteFile(stdin_pipe[WRITE_END], input->str, len_to_write, &len_written, NULL) || len_written != len_to_write) {
		// terminate the running process
		TerminateProcess(pi.hProcess, 1);
		CloseHandle(pi.hProcess);

		CloseHandle(stdout_pipe[READ_END]);
		if (need_stderr) {
			CloseHandle(stderr_pipe[READ_END]);
		}
		CloseHandle(stdin_pipe[WRITE_END]);
		return -1;
	}
    CloseHandle(stdin_pipe[WRITE_END]);

	// read stdout and stderr if needed
	size_t stdout_size = read_pipe(stdout_pipe[READ_END], &stdoutput->str);
	stdoutput->str = ddp_reallocate(stdoutput->str, stdout_size, stdout_size+1);
	stdoutput->str[stdout_size] = '\0';
	stdoutput->cap = stdout_size+1;

	if (need_stderr) {
		size_t stderr_size = read_pipe(stderr_pipe[READ_END], &erroutput->str);
		erroutput->str = ddp_reallocate(erroutput->str, stderr_size, stderr_size+1);
		erroutput->str[stderr_size] = '\0';
		erroutput->cap = stderr_size+1;
	}

	WaitForSingleObject(pi.hProcess, INFINITE);
	DWORD exit_code;
	GetExitCodeProcess(pi.hProcess, &exit_code);
    CloseHandle(pi.hProcess);
    return (ddpint)exit_code;
}
#else // DDPOS_LINUX

// reads everything from the given pipe into out
// then closes the pipe
// returns the new size of out
static size_t read_pipe(int fd, char** out) {
	size_t out_size = 0;
	char buff[BUFF_SIZE];
	int nread;
	while ((nread = read(fd, buff, sizeof(buff))) > 0) {
		*out = ddp_reallocate(*out, out_size, out_size + nread);
		memcpy(*out + out_size, buff, nread);
		out_size += nread;
	}
	close(fd);
	return out_size;
}

// executes path with the given args
// pipes the given input to the processes stdin
// returns the processes stdout and stderr into the given stdoutput
// and erroutput out-variables
// erroutput may be equal to stdoutput if they shall be read together
// but not NULL
static ddpint execute_process(ddpstring* path, ddpstringlist* args,
    ddpstring* input, ddpstringref stdoutput, ddpstringref erroutput)
{
    int stdout_fd[2];
    int stderr_fd[2];
    int stdin_fd[2];

    const bool need_stderr = stdoutput != erroutput;

    // prepare the pipes
    if (pipe(stdout_fd))
        return -1;
    if (need_stderr && pipe(stderr_fd)) {
        close(stdout_fd[0]);
        close(stdout_fd[1]);
        return -1;
    }
    if (pipe(stdin_fd)) {
        close(stdout_fd[0]);
        close(stdout_fd[1]);
        if (need_stderr) {
            close(stderr_fd[0]);
            close(stderr_fd[1]);
        }
        return -1;
    }

    // prepare the arguments
    const size_t argc = args->len + 1;
    char** process_args = ALLOCATE(char*, argc+1); // + 1 for the terminating NULL

    process_args[0] = ALLOCATE(char, strlen(path->str)+1);
    strcpy(process_args[0], path->str);
    for (int i = 1; i < argc; i++) {
        process_args[i] = ALLOCATE(char, strlen(args->arr[i-1].str)+1);
        strcpy(process_args[i], args->arr[i-1].str);
    }
    process_args[argc] = NULL;

    // prepare the output
    FREE_ARRAY(char, stdoutput->str, stdoutput->cap);
    stdoutput->cap = 0;
    stdoutput->str = NULL;
    if (need_stderr) {
        FREE_ARRAY(char, erroutput->str, erroutput->cap);
        erroutput->cap = 0;
        erroutput->str = NULL;
    }

    // create the supprocess
    switch (fork()) {
    case -1: // error
        return -1;
    case 0: { // child
        close(stdout_fd[READ_END]);
        if (need_stderr)
            close(stderr_fd[READ_END]);
        close(stdin_fd[WRITE_END]);
        dup2(stdout_fd[WRITE_END], STDOUT_FILENO);
        dup2(need_stderr ? stderr_fd[WRITE_END] : stdout_fd[WRITE_END], STDERR_FILENO);
        dup2(stdin_fd[READ_END], STDIN_FILENO);
        execv(path->str, process_args);
        return errno;
    }
    default: { // parent
        // free the arguments
        for (int i = 0; i < argc; i++) {
            FREE_ARRAY(char, process_args[i], strlen(process_args[i])+1);
        }
        FREE_ARRAY(char*, process_args, argc+1);
        
        close(stdout_fd[WRITE_END]);
        if (need_stderr)
            close(stderr_fd[WRITE_END]);
        close(stdin_fd[READ_END]);

        if (write(stdin_fd[WRITE_END], input->str, input->cap) < 0) {
            return -1;
        }
        close(stdin_fd[WRITE_END]);

        int exit_code;
        wait(&exit_code);

        size_t stdout_size = read_pipe(stdout_fd[READ_END], &stdoutput->str);
        stdoutput->str = ddp_reallocate(stdoutput->str, stdout_size, stdout_size+1);
        stdoutput->str[stdout_size] = '\0';
        stdoutput->cap = stdout_size+1;

        if (need_stderr) {
            size_t stderr_size = read_pipe(stderr_fd[READ_END], &erroutput->str);
            erroutput->str = ddp_reallocate(erroutput->str, stderr_size, stderr_size+1);
            erroutput->str[stderr_size] = '\0';
            erroutput->cap = stderr_size+1;
        }

        if (WIFEXITED(exit_code)) {
            return (ddpint)WEXITSTATUS(exit_code);
        }
        return -1;
    }
    }
}

#endif // DDPOS_WINDOWS

ddpint Programm_Ausfuehren(ddpstring* ProgrammName, ddpstringlist* Argumente,
    ddpstring* StandardEingabe, ddpstringref StandardAusgabe, ddpstringref StandardFehlerAusgabe) {
    return execute_process(ProgrammName, Argumente, StandardEingabe, StandardAusgabe, StandardFehlerAusgabe);
}