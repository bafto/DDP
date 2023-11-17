#include "ddptypes.h"
#include "ddpwindows.h"
#include "ddpmemory.h"
#include <sys/types.h>
#include <dirent.h>
#include <libgen.h>
#include <stdio.h>
#include <string.h>
#include <time.h>
#include <wait.h>

// copied from https://stackoverflow.com/questions/11238918/s-isreg-macro-undefined
// to handle missing macros on Windows
#define _CRT_INTERNAL_NONSTDC_NAMES 1
#include <sys/stat.h>
#if !defined(S_ISDIR) && defined(S_IFMT) && defined(S_IFDIR)
  #define S_ISDIR(m) (((m) & S_IFMT) == S_IFDIR)
#endif

#ifdef DDPOS_WINDOWS
#include <io.h>
#include <direct.h>
#define access _access
#define stat _stat
#define mkdir _mkdir
#define F_OK 0
#define PATH_SEPERATOR "/\\"// set of possible seperators
#else
#include <unistd.h>
#include <fcntl.h>
#define PATH_SEPERATOR "/"// set of possible seperators
#define mkdir(arg) mkdir(arg, 0700)
#endif // DDPOS_WINDOWS

ddpbool Existiert_Pfad(ddpstring* Pfad) {
	return access(Pfad->str, F_OK) == 0;
}

ddpbool Erstelle_Ordner(ddpstring* Pfad) {
	// recursively create every directory needed to create the final one
	char* it = Pfad->str;
	while ((it = strpbrk(it, PATH_SEPERATOR)) != NULL) {
		*it = '\0';
		if (mkdir(Pfad->str) != 0) return false;
		*it = '/';
		it++;
	}

	// == '/' because it might have already been created
	return Pfad->str[Pfad->cap - 2] == '/' || mkdir(Pfad->str) == 0;
}

ddpbool Ist_Ordner(ddpstring* Pfad) {
	// remove possible trailing seperators
	char* it = Pfad->str + Pfad->cap - 2; // last character in str
	while (it >= Pfad->str) {
		if (strpbrk(it--, PATH_SEPERATOR) != NULL) {
			*(it+1) = '\0';
		} else {
			break;
		}
	}

	struct stat path_stat;
	if (stat(Pfad->str, &path_stat) != 0) return false;
	return S_ISDIR(path_stat.st_mode);
}

// copied from https://stackoverflow.com/questions/2256945/removing-a-non-empty-directory-programmatically-in-c-or-c
static int remove_directory(const char *path) {
	DIR *d = opendir(path);
	size_t path_len = strlen(path);
	int r = -1;

	if (d) {
		struct dirent *p;
		r = 0;
		while (!r && (p = readdir(d))) {
			int r2 = -1;
			char *buf;
			size_t len;

			// Skip the names "." and ".." as we don't want to recurse on them.
			if (!strcmp(p->d_name, ".") || !strcmp(p->d_name, ".."))
				continue;

			len = path_len + strlen(p->d_name) + 2;
			buf = ALLOCATE(char, len);

			if (buf) {
				struct stat statbuf;

				snprintf(buf, len, "%s/%s", path, p->d_name);
				if (!stat(buf, &statbuf)) {
					if (S_ISDIR(statbuf.st_mode))
						r2 = remove_directory(buf);
					else
						r2 = unlink(buf);
				}
				FREE(char, buf);
			}
			r = r2;
		}
		closedir(d);
	}

	if (!r)
		r = rmdir(path);

	return r;
}

ddpbool Loesche_Pfad(ddpstring* Pfad) {
	if (Ist_Ordner(Pfad)) {
		return remove_directory(Pfad->str) == 0;
	}
	return unlink(Pfad->str) == 0;
}

ddpbool Pfad_Verschieben(ddpstring* Pfad, ddpstring* NeuerName) {
	struct stat path_stat;
	// https://stackoverflow.com/questions/64276902/mv-command-implementation-in-c-not-moving-files-to-different-directory
	if (stat(NeuerName->str, &path_stat) == 0 && S_ISDIR(path_stat.st_mode)) {
		char* path_copy = ALLOCATE(char, Pfad->cap);
		memcpy(path_copy, Pfad->str, Pfad->cap);

		char* base = basename(path_copy);
		size_t len_base = strlen(base);

		NeuerName->str = GROW_ARRAY(char, NeuerName->str, NeuerName->cap, NeuerName->cap + len_base + 1);
		strcat(NeuerName->str, "/");
		strcat(NeuerName->str, base);
		NeuerName->cap = NeuerName->cap + len_base + 1;

		FREE(char, path_copy);
	}
	return rename(Pfad->str, NeuerName->str) == 0;
}

static void formatDateStr(ddpstring *str, struct tm *time) {
	// make string
	str->str = NULL;
	str->cap = 0;

	// format string
	char buff[30];
	int size = sprintf(buff, "%02d:%02d:%02d %02d.%02d.%02d", time->tm_hour, time->tm_min, time->tm_sec, time->tm_mday, time->tm_mon + 1, time->tm_year + 1900);

	str->cap = size+1;
	str->str = ALLOCATE(char, str->cap);
	strcpy(str->str, buff);
	str->str[str->cap-1] = '\0';
}

void Zugriff_Datum(ddpstring* ret, ddpstring *Pfad) {
	struct stat st;
	stat(Pfad->str, &st);
	struct tm *tm = localtime(&st.st_atime);

	formatDateStr(ret, tm);
}

void AEnderung_Datum(ddpstring* ret, ddpstring *Pfad) {
	struct stat st;
	stat(Pfad->str, &st);
	struct tm *tm = localtime(&st.st_mtime);

	formatDateStr(ret, tm);
}

void Status_Datum(ddpstring* ret, ddpstring *Pfad) {
	struct stat st;
	stat(Pfad->str, &st);
	struct tm *tm = localtime(&st.st_ctime);

	formatDateStr(ret, tm);
}

ddpint Datei_Groesse(ddpstring *Pfad) {
	struct stat st;
	stat(Pfad->str, &st);

	return (ddpint)st.st_size;
}

ddpint Datei_Modus(ddpstring *Pfad) {
	struct stat st;
	stat(Pfad->str, &st);

	return (ddpint)st.st_mode;
}

// UNIX: https://stackoverflow.com/questions/2180079/how-can-i-copy-a-file-on-unix-using-c
// ToDo: Windows (maybe WinAPI CopyFile())
ddpbool Datei_Kopieren(ddpstring *Pfad, ddpstring *Kopiepfad) {
#ifdef DDPOS_WINDOWS
#error "Datei_Kopieren() not implemented for Windows"
#else // DDPOW_LINUX
    if (!Pfad->str || !Kopiepfad->str) {
        return (ddpbool)false;
    }

	int fd_to, fd_from;

	if ((fd_from = open(Pfad->str, O_RDONLY)) < 0)
		return (ddpbool)false;
	if ((fd_to = open(Kopiepfad->str, O_WRONLY | O_CREAT | O_TRUNC, 0666)) < 0)
		goto out_error;
	
	char buff[1 << 13]; // 8KB
	ssize_t nread;
	while ((nread = read(fd_from, buff, sizeof(buff))) > 0) {
		char* out_ptr = buff;
		do {
            ssize_t nwritten = write(fd_to, out_ptr, nread);

            if (nwritten >= 0) {
                nread -= nwritten;
                out_ptr += nwritten;
            } else if (errno != EINTR) {
                goto out_error;
            }
        } while (nread > 0);
	}

	if (nread == 0) {
		// TODO: check this
		struct stat from_info;
		if (fstat(fd_from, &from_info) >= 0) {
			fchmod(fd_to, from_info.st_mode);
		} 

		close(fd_from);
		close(fd_to);
		return (ddpbool)true;
	}

	out_error:
		close(fd_from);
		if (fd_to >= 0)
			close(fd_to);

	return (ddpbool)false;
#endif // DDPOS_WINDOWS
}