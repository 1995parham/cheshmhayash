import { Injectable } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';

import { Observable, throwError } from 'rxjs';
import { catchError } from 'rxjs/operators';

export interface Connz {
  server_id: string;
}

@Injectable()
export class NATSService {
  constructor(private http: HttpClient) { }

  public list(): Observable<string[]> {
    return this.http.get<string[]>('/api/list');
  }

  public connz(name: string): Observable<Connz> {
    return this.http.get<Connz>('/api/connz', {
      params: {
        name,
      }
    });
  }
}
