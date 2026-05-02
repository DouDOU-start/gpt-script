import { useEffect, useState } from 'react';
import { fetchAccounts } from '../api';
import type { Account } from '../types';

export function useAccounts(showError: (error: unknown) => void) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('active');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(100);
  const [accountTotal, setAccountTotal] = useState(0);
  const [loadingAccounts, setLoadingAccounts] = useState(false);

  const selectedVisibleCount = accounts.filter((account) => selected.has(account.id)).length;
  const totalPages = Math.max(1, Math.ceil(accountTotal / pageSize));

  async function loadAccounts() {
    const params = new URLSearchParams();
    if (search.trim()) params.set('email', search.trim());
    if (statusFilter.trim()) params.set('status', statusFilter.trim());
    params.set('page', String(page));
    params.set('page_size', String(pageSize));
    setLoadingAccounts(true);
    try {
      const data = await fetchAccounts(params);
      setAccounts(data.accounts || []);
      setAccountTotal(data.total || 0);
      setSelected((previous) => new Set(previous));
    } catch (error) {
      showError(error);
    } finally {
      setLoadingAccounts(false);
    }
  }

  function toggleAllVisible(checked: boolean) {
    setSelected((previous) => {
      const next = new Set(previous);
      accounts.forEach((account) => {
        if (checked) next.add(account.id);
        else next.delete(account.id);
      });
      return next;
    });
  }

  function toggleAccount(id: number, checked: boolean) {
    setSelected((previous) => {
      const next = new Set(previous);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  }

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadAccounts();
    }, 250);
    return () => window.clearTimeout(timer);
  }, [search, statusFilter, page, pageSize]);

  useEffect(() => {
    setPage(1);
  }, [search, statusFilter, pageSize]);

  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  return {
    accounts,
    selected,
    search,
    statusFilter,
    page,
    pageSize,
    accountTotal,
    selectedVisibleCount,
    totalPages,
    loadingAccounts,
    setSearch,
    setStatusFilter,
    setPage,
    setPageSize,
    loadAccounts,
    toggleAllVisible,
    toggleAccount,
  };
}
