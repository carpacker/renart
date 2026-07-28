package adbcutil

/*
#include <stdint.h>

struct AdbcStatement;
extern uint8_t AdbcStatementCancel(struct AdbcStatement* statement, void* error);

static uint8_t renart_adbc_statement_cancel(void* statement) {
	return AdbcStatementCancel((struct AdbcStatement*)statement, NULL);
}
*/
import "C"

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"time"
	"unsafe"

	"github.com/apache/arrow-adbc/go/adbc"
)

const (
	adbcStatusOK           = 0
	adbcStatusInvalidState = 6
)

// WatchStatementCancellation bridges context cancellation to the ADBC 1.1
// statement cancellation API. The driver manager currently ignores the
// context passed to ExecuteQuery, so callers that need bounded cancellation
// must explicitly cancel the underlying statement and wait for the watcher to
// unwind.
func WatchStatementCancellation(ctx context.Context, statement adbc.Statement) (func(), error) {
	rawStatement, err := statementPointer(statement)
	if err != nil {
		return nil, err
	}

	finished := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			for {
				status := uint8(C.renart_adbc_statement_cancel(rawStatement))
				runtime.KeepAlive(statement)
				if status == adbcStatusOK || status != adbcStatusInvalidState {
					return
				}
				select {
				case <-finished:
					return
				case <-time.After(time.Millisecond):
				}
			}
		case <-finished:
		}
	}()

	return func() {
		close(finished)
		<-done
	}, nil
}

// statementPointer validates the one implementation detail the ADBC Go driver
// manager does not currently expose: its statement's C handle. Keeping the
// check strict makes an upstream representation change fail closed instead of
// silently making cancellation unreliable.
func statementPointer(statement adbc.Statement) (unsafe.Pointer, error) {
	value := reflect.ValueOf(statement)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return nil, fmt.Errorf("ADBC statement cancellation is unavailable for %T", statement)
	}
	typeInfo := value.Elem().Type()
	if typeInfo.PkgPath() != "github.com/apache/arrow-adbc/go/adbc/drivermgr" || typeInfo.Name() != "stmt" {
		return nil, fmt.Errorf("ADBC statement cancellation is unavailable for %T", statement)
	}
	field := value.Elem().FieldByName("st")
	if !field.IsValid() || field.Kind() != reflect.Pointer || field.IsNil() {
		return nil, fmt.Errorf("ADBC driver-manager statement handle is unavailable")
	}
	return unsafe.Pointer(field.Pointer()), nil
}
