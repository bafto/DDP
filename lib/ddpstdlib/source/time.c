/*
	defines inbuilt ddp functions to work with time
*/
#include <time.h>
#include "ddptypes.h"

ddpint inbuilt_Zeit_Seit_Programmstart() {
	return clock();
}