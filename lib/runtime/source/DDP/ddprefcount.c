#include "DDP/ddprefcount.h"
#include "DDP/ddpmemory.h"
#include "DDP/debug.h"
#include <string.h>
#include <strings.h>

/*
	!!! IMPORTANT !!!

	Remember to use the ULL suffix with any integer literals used in bit operations.
	e.g. (1ULL << index) AND NOT (1 << index).
	THEY ARE NOT EQUIVALENT
*/

#if DDP_ENABLE_REFC_POOL

typedef struct RefcBlock {
	struct RefcBlock *prev;
	struct RefcBlock *next;
	uint64_t used;
	ddpint refcounts[64];
} RefcBlock;

#define ALL_FREE (0ULL)
#define ALL_USED (~(0ULL))

static RefcBlock *refc_root = NULL;

static RefcBlock *new_refc_block(RefcBlock *prev) {
	RefcBlock *block = DDP_ALLOCATE(RefcBlock, 1);
	DDP_DBGLOG("new_refc_block: %p", block);

	block->prev = prev;
	block->next = NULL;
	if (prev != NULL) {
		prev->next = block;
	}

	block->used = ALL_FREE;
	memset(block->refcounts, 0, sizeof(block->refcounts));
	return block;
}

// returns the next block that has free capacity
static RefcBlock *next_block_with_capa(void) {
	if (refc_root == NULL) {
		refc_root = new_refc_block(NULL);
		return refc_root;
	}

	RefcBlock *it = refc_root;
	while (it->used == ALL_USED) {
		if (it->next == NULL) {
			return new_refc_block(it);
		}
		it = it->next;
	}
	return it;
}

static bool refc_in_block(ddpint *refc, RefcBlock *block) {
	const uintptr_t refc_ptr = (uintptr_t)refc;
	const uintptr_t arr_start = (uintptr_t)&block->refcounts[0];
	const uintptr_t arr_end = (uintptr_t)&block->refcounts[63];

	return refc_ptr >= arr_start && refc_ptr <= arr_end;
}

// returns the block that contains this refc
static RefcBlock *get_block_of_refc(ddpint *refc) {
	RefcBlock *it = refc_root;
	while (it != NULL && !refc_in_block(refc, it)) {
		it = it->next;
	}
	return it;
}

// allocates a refc from a block
// may only be called on blocks that have free space
static ddpint *allocate_refc(RefcBlock *block) {
	// get first zero bit in used
	int first_zero = ffsll(~block->used) - 1;
	block->used |= 1ULL << first_zero; // set used bit
	DDP_DBGLOG("allocate_refc: %p", block);
	return &block->refcounts[first_zero];
}

static void free_refc(RefcBlock *block, ddpint *refc) {
	DDP_DBGLOG("free_refc: %p %p", block, refc);
	int index = refc - block->refcounts;
	block->used &= ~(1ULL << (index)); // clear used bit

	// free the unused block
	if (block->used == ALL_FREE) {
		DDP_DBGLOG("block->used == ALL_FREE: %p", block);
		if (block == refc_root) {
			refc_root = refc_root->next;
		}

		if (block->prev != NULL) {
			block->prev->next = block->next;
		}
		if (block->next != NULL) {
			block->next->prev = block->prev;
		}

		DDP_DBGLOG("freeing RefcBlock");
		DDP_FREE(RefcBlock, block);
	}
}

// returns a new refcount
ddpint *ddp_allocate_refcount(void) {
	RefcBlock *block = next_block_with_capa();
	ddpint *refc = allocate_refc(block);

	DDP_DBGLOG("ddp_allocate_refcount: %p", refc);
	return refc;
}

// frees the given refcount
void ddp_free_refcount(ddpint *refc) {
	DDP_DBGLOG("ddp_free_refcount: %p", refc);
	RefcBlock *block = get_block_of_refc(refc);
	if (block == NULL) {
		ddp_runtime_error(1, "refc %p not found in any block", refc);
	}

	free_refc(block, refc);
}

// frees internal memory used for refc allocation
void ddp_free_refc_blocks(void) {
	RefcBlock *it = refc_root;
	while (it != NULL) {
		RefcBlock *to_free = it;
		it = it->next;
		DDP_FREE(RefcBlock, to_free);
	}
}
#else  // DDP_ENABLE_REFC_POOL

// returns a new refcount
ddpint *ddp_allocate_refcount(void) {
	return DDP_ALLOCATE(ddpint, 1);
}

// frees the given refcount
void ddp_free_refcount(ddpint *refc) {
	DDP_FREE(ddpint, refc);
}

// frees internal memory used for refc allocation
void ddp_free_refc_blocks(void) {}
#endif // DDP_ENABLE_REFC_POOL
