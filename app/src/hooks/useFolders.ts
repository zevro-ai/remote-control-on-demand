import { useEffect, useState } from "react";
import { api } from "../api/client";

export function useFolders() {
  const [folders, setFolders] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .get<string[]>("/api/folders")
      .then(setFolders)
      .catch(() => setFolders([]))
      .finally(() => setLoading(false));
  }, []);

  return { folders, loading };
}
