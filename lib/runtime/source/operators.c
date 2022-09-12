/*
	defines functions for ddp operators
*/
#include "ddptypes.h"
#include "memory.h"
#include "gc.h"
#include "debug.h"
#include "utf8/utf8.h"
#include <math.h>
#include <float.h>

ddpint inbuilt_string_length(ddpstring* str) {
	return (ddpint)utf8_strlen(str->str);
}

ddpchar inbuilt_string_index(ddpstring* str, ddpint index) {
	if (index > str->cap || index < 1 || str->cap <= 1) {
		runtime_error(1, "Index außerhalb der Text Länge (Index war %ld, Text Länge war %ld)\n", index, utf8_strlen(str->str));
	}

	size_t i = 0, len = index;
	while (str->str[i] != 0 && len > 1) {
		i += utf8_num_bytes(str->str + i);
		len--;
	}

	if (str->str[i] == 0) {
		runtime_error(1, "Index außerhalb der Text Länge (Index war %ld, Text Länge war %ld)\n", index, utf8_strlen(str->str));
	}

	return utf8_string_to_char(str->str + i);
}

void inbuilt_replace_char_in_string(ddpstring* str, ddpchar ch, ddpint index) {
	if (index > str->cap || index < 1 || str->cap <= 1) {
		runtime_error(1, "Index außerhalb der Text Länge (Index war %ld, Text Länge war %ld)\n", index, utf8_strlen(str->str));
	}

	size_t i = 0, len = index;
	while (str->str[i] != 0 && len > 1) {
		i += utf8_num_bytes(str->str + i);
		len--;
	}

	if (str->str[i] == 0) {
		runtime_error(1, "Index außerhalb der Text Länge (Index war %ld, Text Länge war %ld)\n", index, utf8_strlen(str->str));
	}

	size_t oldCharLen = utf8_num_bytes(str->str + i);
	char newChar[5];
	size_t newCharLen = utf8_char_to_string(newChar, ch);

	if (oldCharLen == newCharLen) { // no need for allocations
		memcpy(str->str + i, newChar, newCharLen);
		return;
	} else if (oldCharLen > newCharLen) { // no need for allocations
		memcpy(str->str + i, newChar, newCharLen);
		memmove(str->str + i + newCharLen, str->str + i + oldCharLen, str->cap - i - oldCharLen);
	} else {
		size_t newStrCap = str->cap - oldCharLen + newCharLen;
		char* newStr = ALLOCATE(char, newStrCap);
		memcpy(newStr, str->str, i); // copy everything before the new char
		memcpy(newStr + i, newChar, newCharLen);
		memcpy(newStr + i + newCharLen, str->str + i + oldCharLen, str->cap - i - oldCharLen);
		FREE_ARRAY(char, str->str, str->cap);
		str->cap = newStrCap;
		str->str = newStr;
	}
}

static ddpint clamp(ddpint i, ddpint min, ddpint max) {
	const ddpint t = i < min ? min : i;
	return t > max ? max : t;
}

ddpstring* inbuilt_string_slice(ddpstring* str, ddpint index1, ddpint index2) {
	DBGLOG("inbuilt_string_slice: %p", str);

	if (str->cap <= 1) {
		return str; // empty string can stay the same
	}

	size_t start_length = utf8_strlen(str->str);
	index1 = clamp(index1, 1, start_length);
	index2 = clamp(index2, 1, start_length);
	if (index2 < index1) {
		runtime_error(1, "Invalide Indexe (Index 1 war %ld, Index 2 war %ld)\n", index1, index2);
	}

	index1--, index2--; // ddp indices start at 1, c indices at 0

	size_t i1 = 0, len = 0;
	while (str->str[i1] != 0 && len != index1) { // while not at null terminator && not at index 1
		++len;
		i1 += utf8_indicated_num_bytes(str->str[i1]);
	}

	size_t i2 = i1;
	while (str->str[i2] != 0 && len != index2) { // while not at null terminator && not at index 2
		++len;
		i2 += utf8_indicated_num_bytes(str->str[i2]);
	}

	size_t new_str_cap = (i2 - i1) + 2; // + 2 because null-terminator
	char* string = ALLOCATE(char, new_str_cap);
	memcpy(string, str->str + i1, new_str_cap - 1);
	string[new_str_cap - 1] = '\0';

	FREE_ARRAY(char, str->str, str->cap); // free the old string
	// assign the new string
	str->cap = new_str_cap;
	str->str = string;
	return str;
}

ddpstring* inbuilt_string_string_verkettet(ddpstring* str1, ddpstring* str2) {
	DBGLOG("inbuilt_string_string_verkettet: %p", str1);

	size_t new_cap = str1->cap - 1 + str2->cap;				 // remove 1 null-terminator
	str1->str = reallocate(str1->str, str1->cap, new_cap);	 // reallocate str1
	memcpy(&str1->str[str1->cap - 1], str2->str, str2->cap); // append str2 and overwrite str1's null-terminator

	str1->cap = new_cap;
	return str1;
}

ddpstring* inbuilt_char_string_verkettet(ddpchar c, ddpstring* str) {
	DBGLOG("inbuilt_char_string_verkettet: %p", str);

	char temp[5];
	int num_bytes = utf8_char_to_string(temp, c);
	if (num_bytes == -1) { // if c is invalid utf8, we return simply a copy of str
		num_bytes = 0;
	}

	size_t new_cap = str->cap + num_bytes;
	str->str = reallocate(str->str, str->cap, new_cap);
	memmove(&str->str[num_bytes], str->str, str->cap);
	memcpy(str->str, temp, num_bytes);

	str->cap = new_cap;
	return str;
}

ddpstring* inbuilt_string_char_verkettet(ddpstring* str, ddpchar c) {
	DBGLOG("inbuilt_string_char_verkettet: %p", str);

	char temp[5];
	int num_bytes = utf8_char_to_string(temp, c);
	if (num_bytes == -1) { // if c is invalid utf8, we return simply a copy of str
		num_bytes = 0;
	}

	size_t new_cap = str->cap + num_bytes;
	str->str = reallocate(str->str, str->cap, new_cap);
	memcpy(&str->str[str->cap - 1], temp, num_bytes);

	str->str[new_cap - 1] = '\0';
	return str;
}

ddpint inbuilt_string_to_int(ddpstring* str) {
	if (str->cap == 0 || str->str[0] == '\0') return 0; // empty string

	return strtoll(str->str, NULL, 10); // cast the copy to int
}

ddpfloat inbuilt_string_to_float(ddpstring* str) {
	if (str->cap == 0 || str->str[0] == '\0') return 0; // empty string

	return strtod(str->str, NULL); // maybe works with comma seperator? We'll see
}

ddpstring* inbuilt_int_to_string(ddpint i) {
	ddpstring* dstr = ALLOCATE(ddpstring, 1); // up here to log the adress in debug mode
	DBGLOG("inbuilt_int_to_string: %p", dstr);

	char buffer[21];
	int len = sprintf(buffer, "%lld", i);

	char* string = ALLOCATE(char, len + 1); // the char array of the string + null-terminator
	memcpy(string, buffer, len);
	string[len] = '\0';

	// set the string fields
	dstr->str = string;
	dstr->cap = len + 1;
	return dstr;
}

ddpstring* inbuilt_float_to_string(ddpfloat f) {
	ddpstring* dstr = ALLOCATE(ddpstring, 1); // up here to log the adress in debug mode
	DBGLOG("inbuilt_float_to_string: %p", dstr);

	char buffer[50];
	int len = sprintf(buffer, "%.16g", f);

	char* string = ALLOCATE(char, len + 1); // the char array of the string + null-terminator
	memcpy(string, buffer, len);
	string[len] = '\0';

	// set the string fields
	dstr->str = string;
	dstr->cap = len + 1;
	return dstr;
}

ddpstring* inbuilt_bool_to_string(ddpbool b) {
	ddpstring* dstr = ALLOCATE(ddpstring, 1); // up here to log the adress in debug mode
	DBGLOG("inbuilt_bool_to_string: %p", dstr);

	char* string;

	if (b) {
		dstr->cap = 5;
		string = ALLOCATE(char, 5);
		memcpy(string, "wahr", 5);
	} else {
		dstr->cap = 7;
		string = ALLOCATE(char, 7);
		memcpy(string, "falsch", 7);
	}
	dstr->str = string;
	return dstr;
}

ddpstring* inbuilt_char_to_string(ddpchar c) {
	ddpstring* dstr = ALLOCATE(ddpstring, 1); // up here to log the adress in debug mode
	DBGLOG("inbuilt_bool_to_string: %p", dstr);

	char temp[5];
	int num_bytes = utf8_char_to_string(temp, c);
	if (num_bytes == -1) { // invalid utf8, string will be empty
		num_bytes = 0;
	}

	char* string = ALLOCATE(char, num_bytes + 1);
	memcpy(string, temp, num_bytes);
	string[num_bytes] = '\0';

	dstr->cap = num_bytes + 1;
	dstr->str = string;
	return dstr;
}

ddpbool inbuilt_string_equal(ddpstring* str1, ddpstring* str2) {
	if (str1 == str2) return true;
	if (strlen(str1->str) != strlen(str2->str)) return false; // if the length is different, it's a quick false return
	return memcmp(str1->str, str2->str, str1->cap) == 0;
}