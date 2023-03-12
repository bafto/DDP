DDP_BIN = ""
STD_BIN = libddpstdlib.a
RUN_BIN = libddpruntime.a
ifeq ($(OS),Windows_NT)
	DDP_BIN := kddp.exe
else
	DDP_BIN := kddp
endif

LLVM_SRC_DIR=./llvm-project/llvm
LLVM_BUILD_DIR=./llvm_build

CC=gcc
CXX=g++
LLVM_BUILD_TYPE=Release
LLVM_CMAKE_GENERATOR="MinGW Makefiles"

OUT_DIR := ./build/DDP

.DEFAULT_GOAL := all

DDP_DIR = ./cmd/kddp
STD_DIR = ./lib/stdlib
RUN_DIR = ./lib/runtime
INCL_DIR = ./lib/runtime/include
DUDEN_DIR = ./lib/stdlib/Duden

DDP_DIR_OUT = $(OUT_DIR)/bin/
LIB_DIR_OUT = $(OUT_DIR)/lib/

CMAKE = cmake
MAKE = make

.PHONY = all debug make_out_dir kddp stdlib stdlib-debug runtime runtime-debug test llvm

all: make_out_dir kddp runtime stdlib

debug: make_out_dir kddp runtime-debug stdlib-debug

kddp:
	cd $(DDP_DIR) ; $(MAKE)
	mv -f $(DDP_DIR)/build/$(DDP_BIN) $(DDP_DIR_OUT)

stdlib:
	cd $(STD_DIR) ; $(MAKE)
	mv -f $(STD_DIR)/$(STD_BIN) $(LIB_DIR_OUT)
	cp -r $(STD_DIR) $(LIB_DIR_OUT)
	rm -rf $(OUT_DIR)/Duden
	mv -f $(LIB_DIR_OUT)stdlib/Duden $(OUT_DIR)

stdlib-debug:
	cd $(STD_DIR) ; $(MAKE) debug
	mv -f $(STD_DIR)/$(STD_BIN) $(LIB_DIR_OUT)
	cp -r $(STD_DIR) $(LIB_DIR_OUT)
	rm -rf $(OUT_DIR)/Duden
	mv -f $(LIB_DIR_OUT)stdlib/Duden $(OUT_DIR)

runtime:
	cd $(RUN_DIR) ; $(MAKE)
	mv -f $(RUN_DIR)/$(RUN_BIN) $(LIB_DIR_OUT)
	cp -r $(RUN_DIR) $(LIB_DIR_OUT)

runtime-debug:
	cd $(RUN_DIR) ; $(MAKE) debug
	mv -f $(RUN_DIR)/$(RUN_BIN) $(LIB_DIR_OUT)
	cp -r $(RUN_DIR) $(LIB_DIR_OUT)

make_out_dir:
	mkdir -p $(OUT_DIR)
	mkdir -p $(DDP_DIR_OUT)
	mkdir -p $(LIB_DIR_OUT)
	cp LICENSE $(OUT_DIR)
	cp README.md $(OUT_DIR)

clean:
	rm -r $(OUT_DIR)

llvm:
# clone the submodule
	git submodule init
	git submodule update

# ignore gopls errors
	cd ./llvm-project ; go mod init ignored || true

# generate cmake build files
	$(CMAKE) -S$(LLVM_SRC_DIR) -B$(LLVM_BUILD_DIR) -DCMAKE_BUILD_TYPE=$(LLVM_BUILD_TYPE) -G$(LLVM_CMAKE_GENERATOR) -DCMAKE_C_COMPILER=$(CC) -DCMAKE_CXX_COMPILER=$(CXX)

# build llvm
	cd $(LLVM_BUILD_DIR) ; $(MAKE)


# will hold the directories to run in the tests
# if empty, all directories are run
TEST_DIRS = 

test:
	go test -v ./tests '-run=(TestKDDP|TestStdlib)' -test_dirs="$(TEST_DIRS)" | sed ''/PASS/s//$$(printf "\033[32mPASS\033[0m")/'' | sed ''/FAIL/s//$$(printf "\033[31mFAIL\033[0m")/''

test-memory:
	go test -v ./tests '-run=(TestMemory)' -test_dirs="$(TEST_DIRS)" | sed ''/PASS/s//$$(printf "\033[32mPASS\033[0m")/'' | sed ''/FAIL/s//$$(printf "\033[31mFAIL\033[0m")/''
