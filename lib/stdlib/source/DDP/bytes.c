#include "DDP/ddpmemory.h"
#include "DDP/ddptypes.h"
#include "DDP/debug.h"
#include <limits.h>
#include <string.h>

// rounds up to the next higher multiple of 8
#define UP_8(n) (((n) + 7) & ~7)
// a number with n ones
#define ONES(n) ((ddpint)((1LL << (ddpint)(n)) - 1LL))

static_assert(sizeof(ddpint) == 8, "sizeof(ddpint) == 8");

typedef struct {
	ddpintlist bytes;
	ddpint len;
} ByteSammlung, *ByteSammlungRef;

#define DDP_EMPTY_BYTES \
	(ByteSammlung){DDP_EMPTY_LIST(ddpintlist), 0}

static ddpint clamp(ddpint i, ddpint min, ddpint max) {
	const ddpint t = i < min ? min : i;
	return t > max ? max : t;
}

static size_t allocate_bytes(ByteSammlung *bytes, size_t n) {
	bytes->len = n;
	size_t needed_bytes = bytes->len;
	if (needed_bytes % sizeof(ddpint) != 0) {
		needed_bytes = UP_8(needed_bytes);
	}
	ddp_ddpintlist_from_constants(&bytes->bytes, needed_bytes / sizeof(ddpint));
	return needed_bytes;
}

void ByteSammlung_Von_Bis(ByteSammlung *ret, ByteSammlung *bytes, ddpint start, ddpint end) {
	if (bytes->len <= 0) {
		*ret = DDP_EMPTY_BYTES;
		return;
	}

	// convert ddp indices to C indices
	start--, end--;

	end = clamp(end, 0, bytes->len);
	start = clamp(start, 0, bytes->len);

	if (end < start) {
		ddp_runtime_error(1, "Invalide Indexe (Index 1 war " DDP_INT_FMT ", Index 2 war " DDP_INT_FMT ")\n", start, end);
	}

	size_t needed_bytes = allocate_bytes(ret, end - start + 1);

	const uint8_t *inBytes = (uint8_t *)bytes->bytes.arr;
	uint8_t *retBytes = (uint8_t *)ret->bytes.arr;

	memcpy(retBytes, &inBytes[start], ret->len);
	memset(&retBytes[ret->len], 0, needed_bytes - ret->len);
}

void ByteSammlung_Verkettet(ByteSammlung *ret, ByteSammlung *a, ByteSammlung *b) {
	size_t needed_bytes = allocate_bytes(ret, a->len + b->len);

	const uint8_t *aBytes = (uint8_t *)a->bytes.arr;
	const uint8_t *bBytes = (uint8_t *)a->bytes.arr;
	uint8_t *retBytes = (uint8_t *)ret->bytes.arr;

	memcpy(retBytes, aBytes, a->len);
	memcpy(&retBytes[a->len], bBytes, b->len);
	memset(&retBytes[ret->len], 0, needed_bytes - ret->len);
}

void Zahl_Als_ByteSammlung(ByteSammlung *ret, ddpint z) {
	ddp_ddpintlist_from_constants(&ret->bytes, 1);
	ret->len = sizeof(ddpint);
	ret->bytes.arr[0] = z;
}

ddpint ByteSammlung_Als_Zahl(ByteSammlung *b) {
	if (b->len >= 8) {
		return b->bytes.arr[0];
	}

	return b->bytes.arr[0] & ONES(b->len * 8);
}
