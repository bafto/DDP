#include "DDP/ddprefcount.h"
#include "DDP/ddpmemory.h"
#include "DDP/debug.h"
#include <string.h>
#include <strings.h>
#include <time.h>

/*
	!!! IMPORTANT !!!

	Remember to use the ULL suffix with any integer literals used in bit operations.
	e.g. (1ULL << index) AND NOT (1 << index).
	THEY ARE NOT EQUIVALENT
*/

static struct timespec bench_start(void) {
#ifdef DDP_DEBUG
	struct timespec tstart = {0, 0};
	clock_gettime(CLOCK_MONOTONIC, &tstart);
	return tstart;
#else
	return (struct timespec){0, 0};
#endif
}

static void bench_end(struct timespec tstart, const char *op) {
#ifdef DDP_DEBUG
	(void)op;
	(void)tstart;
	struct timespec tend = {0, 0};
	clock_gettime(CLOCK_MONOTONIC, &tend);
	DDP_DBGLOG("%s took %.2f microseconds", op,
			   ((double)tend.tv_sec * 1.0e6 + 1.0e-3 * tend.tv_nsec) -
				   ((double)tstart.tv_sec * 1.0e6 + 1.0e-3 * tstart.tv_nsec));
#endif
}

#if DDP_ENABLE_REFC_POOL

#define REFC_PER_BLOCK 64

typedef struct RefcBlock {
	struct RefcBlock *prev;
	struct RefcBlock *next;
	uint64_t used;
	ddpint refcounts[REFC_PER_BLOCK];
} RefcBlock;

#define ALL_FREE (0ULL)
#define ALL_USED (~(0ULL))

static RefcBlock *refc_root = NULL;
static RefcBlock *refc_end = NULL;

#define CACHED_BLOCKS 16 // roughly 8KB
static RefcBlock *block_cache[CACHED_BLOCKS] = {0};

static RefcBlock *new_refc_block(void) {
	RefcBlock *block = NULL;
	for (int i = 0; i < CACHED_BLOCKS; i++) {
		if (block_cache[i] != NULL) {
			block = block_cache[i];
			block_cache[i] = NULL;
			break;
		}
	}
	if (block == NULL) {
		block = DDP_ALLOCATE(RefcBlock, 1);
	}

	block->prev = NULL;
	if (refc_end != NULL) {
		refc_end->next = block;
		block->prev = refc_end;
	}
	block->next = NULL;

	refc_end = block;

	block->used = ALL_FREE;
	return block;
}

static void free_refc_block(RefcBlock *block) {
	if (block == refc_root) {
		refc_root = refc_root->next;
	}
	if (block == refc_end) {
		refc_end = refc_end->prev;
	}

	if (block->prev != NULL) {
		block->prev->next = block->next;
	}
	if (block->next != NULL) {
		block->next->prev = block->prev;
	}

	for (int i = 0; i < CACHED_BLOCKS; i++) {
		if (block_cache[i] == NULL) {
			block_cache[i] = block;
			return;
		}
	}
	DDP_FREE(RefcBlock, block);
}

// returns the next block that has free capacity
static RefcBlock *next_block_with_capa(void) {
	if (refc_root == NULL) {
		refc_root = new_refc_block();
		return refc_root;
	}

	RefcBlock *it = refc_end;
	while (it->used == ALL_USED) {
		if (it->prev == NULL) {
			return new_refc_block();
		}
		it = it->prev;
	}
	return it;
}

static bool refc_in_block(ddpint *refc, RefcBlock *block) {
	const uintptr_t refc_ptr = (uintptr_t)refc;
	const uintptr_t arr_start = (uintptr_t)&block->refcounts[0];
	const uintptr_t arr_end = (uintptr_t)&block->refcounts[REFC_PER_BLOCK - 1];

	return refc_ptr >= arr_start && refc_ptr <= arr_end;
}

// returns the block that contains this refc
static RefcBlock *get_block_of_refc(ddpint *refc) {
	RefcBlock *it = refc_end;
	while (it != NULL && !refc_in_block(refc, it)) {
		it = it->prev;
	}
	return it;
}

// allocates a refc from a block
// may only be called on blocks that have free space
static ddpint *allocate_refc(RefcBlock *block) {
	// get first zero bit in used
	int first_zero = ffsll(~block->used) - 1;
	block->used |= 1ULL << first_zero; // set used bit
	return &block->refcounts[first_zero];
}

static void free_refc(RefcBlock *block, ddpint *refc) {
	int index = refc - block->refcounts;
	block->used &= ~(1ULL << (index)); // clear used bit

	// free the unused block
	if (block->used == ALL_FREE) {
		free_refc_block(block);
	}
}

#endif // DDP_ENABLE_REFC_POOL

// returns a new refcount
ddpint *ddp_allocate_refcount(void) {
	ddpint *refc;
#if DDP_ENABLE_REFC_POOL
	struct timespec start = bench_start();
	RefcBlock *block = next_block_with_capa();
	bench_end(start, "next_block_with_capa");

	start = bench_start();
	refc = allocate_refc(block);
	bench_end(start, "allocate_refc");
#else
	refc = DDP_ALLOCATE(ddpint, 1);
#endif
	return refc;
}

// frees the given refcount
void ddp_free_refcount(ddpint *refc) {

#if DDP_ENABLE_REFC_POOL
	struct timespec start = bench_start();
	RefcBlock *block = get_block_of_refc(refc);
	if (block == NULL) {
		ddp_runtime_error(1, "refc %p not found in any block", refc);
	}
	bench_end(start, "get_block_of_refc");

	start = bench_start();
	free_refc(block, refc);
	bench_end(start, "free_refc");
#else
	DDP_FREE(ddpint, refc);
#endif
}

// frees internal memory used for refc allocation
void ddp_free_refc_blocks(void) {
#if DDP_ENABLE_REFC_POOL
	RefcBlock *it = refc_root;
	while (it != NULL) {
		RefcBlock *to_free = it;
		it = it->next;
		DDP_FREE(RefcBlock, to_free);
	}

	refc_root = NULL;
	refc_end = NULL;

	for (int i = 0; i < CACHED_BLOCKS; i++) {
		if (block_cache[i] != NULL) {
			DDP_FREE(RefcBlock, block_cache[i]);
		}
	}
#endif
}
