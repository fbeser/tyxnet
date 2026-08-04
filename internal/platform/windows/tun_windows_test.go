//go:build windows

package windows

import "testing"

func TestTyxNetAdapterGUIDIsStable(t *testing.T) {
	if tyxNetAdapterGUID.Data1 != 0x7f236a91 || tyxNetAdapterGUID.Data2 != 0x99e4 || tyxNetAdapterGUID.Data3 != 0x54bd {
		t.Fatalf("unexpected adapter GUID: %+v", tyxNetAdapterGUID)
	}
}

func TestClientAdapterGUIDIsStableAndDistinct(t *testing.T) {
	first := adapterGUID("TyxC-12345678")
	if first != adapterGUID("TyxC-12345678") {
		t.Fatal("client adapter GUID is not stable")
	}
	if first == tyxNetAdapterGUID || first == adapterGUID("TyxC-87654321") {
		t.Fatal("distinct adapters received the same GUID")
	}
}
