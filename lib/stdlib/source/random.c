#include "ddptypes.h"
#include "mt19937-64.h"
#include <math.h>

ddpfloat ZufallFloat(ddpfloat a, ddpfloat b) {
	return (b - a) * genrand64_real1() + a;
}

ddpint ZufallInt(ddpint a, ddpint b) {
	return (ddpint)(genrand64_int64() % ((b) - (a+1)) + (a+1));
}

ddpbool ZufallBool(ddpfloat p) {
	if (p < 0) {
		return 0; // for 0% or below it is always false
	} else if (p > 100) {
		return true; // for 100% or above it is always true
	}
	return (genrand64_real1() * 100) < p;
}