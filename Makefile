CMD_DIR = ./cmd/
STD_DIR = ./lib/stdlib/
RUN_DIR = ./lib/runtime/

DDP_SETUP_BUILD_DIR = $(CMD_DIR)ddp-setup/build/

KDDP_BIN = ""
DDP_SETUP_BIN = ""

STD_BIN = libddpstdlib.a
STD_BIN_PCRE2 = libpcre2-8.a
PCRE2_DIR = $(STD_DIR)pcre2/
STD_BIN_DEBUG = $(STD_BIN:.a=debug.a)
RUN_BIN = libddpruntime.a
RUN_BIN_DEBUG = $(RUN_BIN:.a=debug.a)
RUN_BIN_MAIN_DIR = $(RUN_DIR)source/
RUN_BIN_MAIN = main.o
RUN_BIN_MAIN_DEBUG = $(RUN_BIN_MAIN:.o=_debug.o)
DDP_LIST_DEFS_NAME = ddp_list_types_defs

DDP_LIST_DEFS_OUTPUT_TYPES = --llvm-ir --object

LLVM_SRC_DIR=./llvm-project/llvm/
LLVM_BUILD_DIR=./llvm_build/

CC=gcc
CXX=g++

RM = rm -rf
CP = cp -rf
MKDIR = mkdir -p
SED = sed -u
TAR = tar

LLVM_BUILD_TYPE=Release
LLVM_CMAKE_GENERATOR="MinGW Makefiles"
LLVM_CMAKE_BUILD_TOOL=$(MAKE)
LLVM_TARGETS="X86;AArch64"
LLVM_ADDITIONAL_CMAKE_VARIABLES= -DCMAKE_INSTALL_PREFIX=llvm_build/  -DLLVM_BUILD_TOOLS=OFF -DLLVM_ENABLE_BINDINGS=OFF -DLLVM_ENABLE_UNWIND_TABLES=OFF -DLLVM_INCLUDE_BENCHMARKS=OFF -DLLVM_INCLUDE_EXAMPLES=OFF -DLLVM_INCLUDE_TESTS=OFF

ifeq ($(OS),Windows_NT)
	KDDP_BIN = kddp.exe
	DDP_SETUP_BIN = ddp-setup.exe
else
	KDDP_BIN = kddp
	DDP_SETUP_BIN = ddp-setup
	LLVM_CMAKE_GENERATOR="Unix Makefiles"
endif

OUT_DIR = ./build/DDP/

.DEFAULT_GOAL = all

KDDP_DIR_OUT = $(OUT_DIR)bin/
DDP_SETUP_DIR_OUT = $(OUT_DIR)
LIB_DIR_OUT = $(OUT_DIR)lib/
STD_DIR_OUT = $(LIB_DIR_OUT)stdlib/
RUN_DIR_OUT = $(LIB_DIR_OUT)runtime/

CMAKE = cmake

SHELL = /bin/bash
.SHELLFLAGS = -o pipefail -c

.PHONY = all clean clean-outdir debug kddp stdlib stdlib-debug runtime runtime-debug test test-memory checkout-llvm llvm help test-complete test-with-optimizations coverage

all: $(OUT_DIR) kddp runtime stdlib ddp-setup ## compiles kdddp, the runtime, the stdlib and ddp-setup into the build/DDP/ directory 

debug: $(OUT_DIR) kddp runtime-debug stdlib-debug ## same as all but the runtime and stdlib print debugging information

kddp: ## compiles kddp into build/DDP/bin/
	@echo "building kddp"
	cd $(CMD_DIR) ; '$(MAKE)' kddp
	$(CP) $(CMD_DIR)kddp/build/$(KDDP_BIN) $(KDDP_DIR_OUT)$(KDDP_BIN)
	$(KDDP_DIR_OUT)$(KDDP_BIN) dump-list-defs -o $(LIB_DIR_OUT)$(DDP_LIST_DEFS_NAME) $(DDP_LIST_DEFS_OUTPUT_TYPES)

ddp-setup: ## compiles ddp-setup into build/DDP/bin/
	@echo "building ddp-setup"
	cd $(CMD_DIR) ; '$(MAKE)' ddp-setup
	$(CP) $(CMD_DIR)ddp-setup/build/$(DDP_SETUP_BIN) $(DDP_SETUP_DIR_OUT)$(DDP_SETUP_BIN)

stdlib: ## compiles the stdlib and the Duden into build/DDP/lib/stdlib and build/DDP/Duden
	@echo "building the ddp-stdlib"
	cd $(STD_DIR) ; '$(MAKE)'
	$(CP) $(STD_DIR)$(STD_BIN) $(LIB_DIR_OUT)$(STD_BIN)
	@if [ -f $(STD_DIR)$(STD_BIN_PCRE2) ]; then \
		$(CP) $(STD_DIR)$(STD_BIN_PCRE2) $(LIB_DIR_OUT)$(STD_BIN_PCRE2); \
	fi
	$(CP) $(STD_DIR)include/ $(STD_DIR_OUT)
	$(CP) $(STD_DIR)source/ $(STD_DIR_OUT)
	$(CP) $(STD_DIR)Duden/ $(OUT_DIR)
	@if [ -d $(PCRE2_DIR) ]; then \
		$(CP) $(PCRE2_DIR) $(STD_DIR_OUT); \
	fi
	$(CP) $(STD_DIR)Makefile $(STD_DIR_OUT)Makefile

stdlib-debug: ## same as stdlib but will print debugging information
	@echo "building the ddp-stdlib in debug mode"
	cd $(STD_DIR) ; '$(MAKE)' debug
	$(CP) $(STD_DIR)$(STD_BIN_DEBUG) $(LIB_DIR_OUT)$(STD_BIN)
	@if [ -f $(STD_DIR)$(STD_BIN_PCRE2) ]; then \
		$(CP) $(STD_DIR)$(STD_BIN_PCRE2) $(LIB_DIR_OUT)$(STD_BIN_PCRE2); \
	fi
	$(CP) $(STD_DIR)include/ $(STD_DIR_OUT)
	$(CP) $(STD_DIR)source/ $(STD_DIR_OUT)
	$(CP) $(STD_DIR)Duden/ $(OUT_DIR)
	@if [ -d $(PCRE2_DIR) ]; then \
		$(CP) $(PCRE2_DIR) $(STD_DIR_OUT); \
	fi
	$(CP) $(STD_DIR)Makefile $(STD_DIR_OUT)Makefile

runtime: ## compiles the runtime into build/DDP/lib/stdlib
	@echo "building the ddp-runtime"
	cd $(RUN_DIR) ; '$(MAKE)'
	$(CP) $(RUN_DIR)$(RUN_BIN) $(LIB_DIR_OUT)$(RUN_BIN)
	$(CP) $(RUN_BIN_MAIN_DIR)$(RUN_BIN_MAIN) $(LIB_DIR_OUT)$(RUN_BIN_MAIN)
	$(CP) $(RUN_DIR)include/ $(RUN_DIR_OUT)
	$(CP) $(RUN_DIR)source/ $(RUN_DIR_OUT)
	$(CP) $(RUN_DIR)Makefile $(RUN_DIR_OUT)Makefile

runtime-debug: ## same as runtime but prints debugging information
	@echo "building the ddp-runtime in debug mode"
	cd $(RUN_DIR) ; '$(MAKE)' debug
	@echo copying $(RUN_DIR)$(RUN_BIN_DEBUG) to $(LIB_DIR_OUT)$(RUN_BIN)
	$(CP) $(RUN_DIR)$(RUN_BIN_DEBUG) $(LIB_DIR_OUT)$(RUN_BIN)
	$(CP) $(RUN_BIN_MAIN_DIR)$(RUN_BIN_MAIN_DEBUG) $(LIB_DIR_OUT)$(RUN_BIN_MAIN)
	$(CP) $(RUN_DIR)include/ $(RUN_DIR_OUT)
	$(CP) $(RUN_DIR)source/ $(RUN_DIR_OUT)
	$(CP) $(RUN_DIR)Makefile $(RUN_DIR_OUT)Makefile

$(OUT_DIR): LICENSE README.md ## creates build/DDP/, build/DDP/bin/, build/DDP/lib/, ... and copies the LICENSE and README.md
	@echo "creating output directories"
	$(MKDIR) $(OUT_DIR)
	$(MKDIR) $(DDP_SETUP_DIR_OUT)
	$(MKDIR) $(KDDP_DIR_OUT)
	$(MKDIR) $(OUT_DIR)Duden/
	$(MKDIR) $(STD_DIR_OUT)include/
	$(MKDIR) $(STD_DIR_OUT)source/
	$(MKDIR) $(RUN_DIR_OUT)include/
	$(MKDIR) $(RUN_DIR_OUT)source/
	$(CP) LICENSE $(OUT_DIR)
	$(CP) README.md $(OUT_DIR)

clean: clean-outdir ## deletes everything produced by this Makefile
	cd $(CMD_DIR) ; '$(MAKE)' clean
	cd $(STD_DIR) ; '$(MAKE)' clean
	cd $(RUN_DIR) ; '$(MAKE)' clean

clean-outdir: ## deletes build/DDP/
	@echo "deleting output directory"
	$(RM) $(OUT_DIR)

checkout-llvm: ## clones the llvm-project submodule
# clone the submodule
	@echo "cloning the llvm repo"
	git submodule update --init llvm-project

# ignore gopls errors
	cd ./llvm-project ; go mod init ignored || true

llvm: checkout-llvm ## compiles llvm
# generate cmake build files
	@echo "building llvm"
	$(CMAKE) -S$(LLVM_SRC_DIR) -B$(LLVM_BUILD_DIR) -DCMAKE_BUILD_TYPE=$(LLVM_BUILD_TYPE) -G$(LLVM_CMAKE_GENERATOR) -DCMAKE_C_COMPILER=$(CC) -DCMAKE_CXX_COMPILER=$(CXX) -DLLVM_TARGETS_TO_BUILD=$(LLVM_TARGETS) $(LLVM_ADDITIONAL_CMAKE_VARIABLES)

# build llvm
	cd $(LLVM_BUILD_DIR) ; MAKEFLAGS='$(MAKEFLAGS)' $(CMAKE) --build . --target llvm-libraries llvm-config llvm-headers install-llvm-headers

# will hold the directories to run in the tests
# if empty, all directories are run
TEST_DIRS = 
# will hold additional arguments to pass to kddp
KDDP_ARGS = 

test-normal: ## runs the tests
	go test -v ./tests '-run=(TestKDDP|TestStdlib|TestBuildExamples|TestStdlibCoverage)' -test_dirs="$(TEST_DIRS)" -kddp_args="$(KDDP_ARGS)" | $(SED) ''/PASS/s//$$(printf "\033[32mPASS\033[0m")/'' | $(SED) ''/FAIL/s//$$(printf "\033[31mFAIL\033[0m")/''

test-memory: ## runs the tests checking for memory leaks
	go test -v ./tests '-run=(TestMemory)' -test_dirs="$(TEST_DIRS)" -kddp_args="$(KDDP_ARGS)" | $(SED) -u ''/PASS/s//$$(printf "\033[32mPASS\033[0m")/'' | $(SED) -u ''/FAIL/s//$$(printf "\033[31mFAIL\033[0m")/''

test-normal-memory: ## runs test-normal and test-memory in the correct order
	'$(MAKE)' all 
	'$(MAKE)' test-normal 
	'$(MAKE)' debug 
	'$(MAKE)' test-memory

test-sumtypes: ## validates that sumtypes in the source tree are correctly used
	go run github.com/BurntSushi/go-sumtype@latest $(shell go list ./... | grep -v vendor)

coverage: ## creates a coverage report for tests/testdata/stdlib
	go test -v ./tests '-run=TestStdlibCoverage' | $(SED) -u ''/PASS/s//$$(printf "\033[32mPASS\033[0m")/'' | $(SED) -u ''/FAIL/s//$$(printf "\033[31mFAIL\033[0m")/''

test: test-sumtypes test-normal-memory coverage ## runs all the tests

test-with-optimizations: ## runs all tests with full optimizations enabled
	'$(MAKE)' KDDP_ARGS="-O 2" test

help: ## Show this help.
	@egrep -h '\s##\s' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m  %-30s\033[0m %s\n", $$1, $$2}'
