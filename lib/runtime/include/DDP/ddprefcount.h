#ifndef DDP_REFCOUNT_H
#define DDP_REFCOUNT_H

#include "ddptypes.h"

// returns a new refcount
ddpint *ddp_allocate_refcount(void);
// frees the given refcount
void ddp_free_refcount(ddpint *refc);

// frees internal memory used for refc allocation
void ddp_free_refc_blocks(void);

#endif // DDP_REFCOUNT_H
